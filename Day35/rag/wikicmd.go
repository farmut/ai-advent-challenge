package main

// wikicmd.go holds everything the CLI needs to talk to the wiki API: flag
// wiring, token resolution and the `rag wiki probe` subcommand.
//
// The one rule that shapes this file: THE TOKEN NEVER PASSES THROUGH ARGV.
// There is deliberately no --token flag and there never will be one. A command
// line is visible in `ps` to every user on the box, is written to shell history,
// is readable at /proc/<pid>/cmdline, and is echoed verbatim into CI logs that
// are often world-readable inside a company. A leaked wiki token reads the whole
// corporate wiki. The token comes from the environment, or from a file whose
// permissions are checked first.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rag/internal/source/wiki"
)

// envWikiToken is the only supported way to hand the crawler a token without
// touching the filesystem.
const envWikiToken = "WIKI_OAUTH_TOKEN"

// orgHeaders are the two headers the API accepts, and why each exists.
var orgHeaders = map[string]string{
	"X-Org-Id":       "Yandex 360 for Business",
	"X-Cloud-Org-Id": "Yandex Cloud Organization",
}

// ---------------------------------------------------------------------------
// repeatable flags
// ---------------------------------------------------------------------------

// stringList collects a flag that may be given more than once
// (--root a --root b). flag.Value's Set is called once per occurrence.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return errors.New("empty value")
	}
	*l = append(*l, v)
	return nil
}

// ---------------------------------------------------------------------------
// flags
// ---------------------------------------------------------------------------

// wikiFlags are the --source wiki options of `rag index`.
type wikiFlags struct {
	APIURL      string
	PageBaseURL string
	OrgID       string
	OrgHeader   string
	TokenFile   string
	DumpDir     string

	Roots     stringList
	Allow     stringList
	Deny      stringList
	PageTypes stringList
	AllowFile string
	DenyFile  string
	ACLMode   string

	MaxPages    int
	MaxDepth    int
	MaxRequests int
	MaxBytes    int64
}

func addWikiIndexFlags(fs *flag.FlagSet) *wikiFlags {
	f := &wikiFlags{}

	fs.StringVar(&f.APIURL, "api-url", wiki.DefaultAPIURL, "wiki API base URL (requests only, never used for page links)")
	fs.StringVar(&f.PageBaseURL, "page-base-url", "", "REQUIRED for --source wiki: browser base for page links, e.g. https://wiki.yandex.ru (no default on purpose)")
	fs.StringVar(&f.OrgID, "org-id", "", "organization id (required for --source wiki)")
	fs.StringVar(&f.OrgHeader, "org-header", "X-Org-Id", "header carrying the org id: X-Org-Id (360 for Business) | X-Cloud-Org-Id (Cloud Org)")
	fs.StringVar(&f.TokenFile, "token-file", "", "file holding the OAuth token; must be chmod 600. Prefer the "+envWikiToken+" env var")
	fs.StringVar(&f.DumpDir, "dump-dir", "", "directory for redacted diagnostic dumps (created 0700, files 0600)")

	fs.Var(&f.Roots, "root", "wiki slug to crawl recursively; repeat for several roots")
	fs.Var(&f.Allow, "allow", "slug prefix to include; repeat. FAIL-CLOSED: with no --allow nothing is indexed")
	fs.Var(&f.Deny, "deny", "slug prefix to exclude; repeat. Deny always beats allow")
	fs.Var(&f.PageTypes, "page-type", "page_type value to index; repeat. Default: every type. "+
		"Run --dry-run first: the manifest lists each page's actual type, which is what to narrow this to")
	fs.StringVar(&f.AllowFile, "allow-file", "", "file with one allow prefix per line (# comments allowed)")
	fs.StringVar(&f.DenyFile, "deny-file", "", "file with one deny prefix per line (# comments allowed)")
	fs.StringVar(&f.ACLMode, "acl-mode", string(wiki.DefaultACLMode),
		"page permission policy: require (fail-closed; indexes NOTHING while the API returns 405 for /access) | "+
			"probe (default: try, and continue with acl=unchecked if the endpoint is absent) | off (do not check at all)")

	fs.IntVar(&f.MaxPages, "max-pages", 0, "abort if the crawl exceeds this many pages (0 = unlimited)")
	fs.IntVar(&f.MaxDepth, "max-depth", 0, "abort if the crawl goes deeper than this (0 = unlimited)")
	fs.IntVar(&f.MaxRequests, "max-requests", 0, "abort after this many HTTP requests (0 = unlimited)")
	fs.Int64Var(&f.MaxBytes, "max-bytes", 0, "abort once this much content has been fetched (0 = unlimited)")

	return f
}

// ---------------------------------------------------------------------------
// token
// ---------------------------------------------------------------------------

// resolveWikiToken returns the OAuth token, from the environment first and a
// permission-checked file second.
func resolveWikiToken(tokenFile string) (string, error) {
	if t := strings.TrimSpace(os.Getenv(envWikiToken)); t != "" {
		return t, nil
	}

	if strings.TrimSpace(tokenFile) == "" {
		return "", fmt.Errorf(`no wiki OAuth token.

  Provide one with:
    export %s='<token>'

  The token must belong to a USER account — the wiki API does not authorize
  service accounts.

  Alternatively pass --token-file <path> to a file containing only the token,
  with permissions 600.

  There is deliberately no --token flag: a command line is visible in ps output,
  in shell history, at /proc/<pid>/cmdline and in CI logs.`, envWikiToken)
	}

	info, err := os.Stat(tokenFile)
	if err != nil {
		return "", fmt.Errorf("--token-file %s: %w", tokenFile, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("--token-file %s is not a regular file", tokenFile)
	}
	// Group/other must have no access at all. A token file anyone can read is
	// the same leak as putting the token in argv, just slower to notice.
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return "", fmt.Errorf("--token-file %s has mode %04o: it is readable or writable by group or other.\n"+
			"  A token file must be private. Fix it with:\n    chmod 600 %s",
			tokenFile, perm, tokenFile)
	}

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("--token-file %s: %w", tokenFile, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("--token-file %s is empty", tokenFile)
	}
	return token, nil
}

// ---------------------------------------------------------------------------
// wiki source construction
// ---------------------------------------------------------------------------

// buildWikiSource validates the flags and wires up the crawler.
//
// The ORDER of the checks is part of the contract: everything that can be
// decided locally is decided before a token is read and long before a socket is
// opened. A misconfigured run must fail instantly and say what is missing, not
// after authenticating against a corporate API.
func buildWikiSource(f *wikiFlags, dryRun bool, log io.Writer) (*wiki.Source, error) {
	if len(f.Roots) == 0 {
		return nil, errors.New("--root is required for --source wiki (repeat it to crawl several subtrees)")
	}

	if strings.TrimSpace(f.PageBaseURL) == "" {
		return nil, fmt.Errorf(`--page-base-url is required for --source wiki and has no default.

  It is the BROWSER base for page links, e.g.
    --page-base-url https://wiki.yandex.ru
  or your organization's own wiki domain.

  It is intentionally separate from --api-url (%s): that is an API host, and a
  link built from it does not open a page for a human. A support bot handing out
  broken source links is not something anyone notices quickly, so this value is
  never guessed.`, wiki.DefaultAPIURL)
	}

	if err := validateOrgHeader(f.OrgHeader); err != nil {
		return nil, err
	}
	aclMode, err := wiki.ParseACLMode(f.ACLMode)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(f.OrgID) == "" {
		return nil, errors.New("--org-id is required for --source wiki (the API needs it to resolve the organization; without it every request fails with 403)")
	}

	// The roots are already validated above, so this suggestion is always a
	// command the user can paste.
	allow, err := mergeRules(f.Allow, f.AllowFile, "allow")
	if err != nil {
		return nil, err
	}
	deny, err := mergeRules(f.Deny, f.DenyFile, "deny")
	if err != nil {
		return nil, err
	}

	// Globs are refused before anything else looks at the rules: "*" is the other
	// thing a person types for "everything", and unlike "/" it would survive
	// normalisation as a literal rule matching nothing at all.
	if globs := wiki.GlobRules(append(append([]string{}, allow...), deny...)); len(globs) > 0 {
		return nil, fmt.Errorf("rule %q looks like a glob, and glob patterns are not supported.\n"+
			"  Rules are literal slug prefixes. Taken literally this one matches no page,\n"+
			"  so the run would walk the wiki, index nothing, and still look configured.\n"+
			"  There is no wildcard on purpose: it is the one flag that could hand the whole\n"+
			"  corporate wiki to a support chat by accident.\n"+
			"  Name the sections you actually want:\n"+
			"    %s\n"+
			"  A prefix already covers everything beneath it: --allow docs takes docs/api too.",
			globs[0], allowFlagsFor(f.Roots))
	}

	// Refuse a root-level rule loudly instead of letting NewFilter drop it. The
	// filter would then be silently empty, and the run would fail closed with
	// "no_allow_rule" — correct, but it reads as "you forgot --allow" to someone
	// who is looking straight at the --allow they typed. Naming the rejected rule
	// is the difference between a two-second fix and a hunt.
	allow, droppedAllow := wiki.NormalizeRules(allow)
	if len(droppedAllow) > 0 {
		return nil, fmt.Errorf("--allow %q means the wiki root, and there is no wildcard on purpose.\n"+
			"  A root-level rule is a prefix of every slug, so it would turn the fail-closed\n"+
			"  filter into a full crawl of the whole wiki — the accident this design exists\n"+
			"  to prevent.\n"+
			"  Name the sections you actually want:\n"+
			"    %s\n"+
			"  Listing them also puts them in the manifest, where a reviewer can see them.",
			droppedAllow[0], allowFlagsFor(f.Roots))
	}
	// A root-level deny is the mirror image: it excludes everything, so the run
	// would index nothing while looking configured.
	deny, droppedDeny := wiki.NormalizeRules(deny)
	if len(droppedDeny) > 0 {
		return nil, fmt.Errorf("--deny %q means the wiki root, which excludes every page and would index nothing.\n"+
			"  Name the subtrees to exclude, e.g. --deny docs/hr --deny docs/legal.", droppedDeny[0])
	}

	if len(allow) == 0 {
		return nil, fmt.Errorf("no allow rules: the filter is fail-closed, so nothing would be indexed.\n"+
			"  Name the subtrees to index explicitly, with either flag:\n"+
			"    --allow docs/public --allow howto\n"+
			"    --allow-file <file with one prefix per line>\n"+
			"  To index everything under the roots you are crawling, allow the roots themselves:\n"+
			"    %s\n"+
			"  --root says where to start walking; --allow says what to keep. They are separate\n"+
			"  because a walk can descend into a neighbouring subtree, so the root is not a boundary.\n"+
			"  (an empty allow list means \"nothing\" on purpose: a forgotten flag must not crawl the whole wiki)",
			allowFlagsFor(f.Roots))
	}

	token, err := resolveWikiToken(f.TokenFile)
	if err != nil {
		return nil, err
	}

	client, err := wiki.NewClient(wiki.ClientConfig{
		APIURL:      f.APIURL,
		Token:       token,
		OrgID:       f.OrgID,
		OrgHeader:   f.OrgHeader,
		Timeout:     30 * time.Second,
		MaxRetries:  3,
		MinInterval: 100 * time.Millisecond,
		MaxRequests: f.MaxRequests,
	})
	if err != nil {
		return nil, err
	}

	return wiki.New(wiki.Config{
		Client:      client,
		Filter:      wiki.NewFilter(allow, deny),
		Roots:       f.Roots,
		PageTypes:   f.PageTypes,
		ACLMode:     aclMode,
		PageBaseURL: f.PageBaseURL,
		DryRun:      dryRun,
		Log:         log,
		Limits: wiki.Limits{
			MaxPages:    f.MaxPages,
			MaxDepth:    f.MaxDepth,
			MaxRequests: f.MaxRequests,
			MaxBytes:    f.MaxBytes,
		},
	})
}

func validateOrgHeader(h string) error {
	if _, ok := orgHeaders[h]; ok {
		return nil
	}
	return fmt.Errorf("unknown --org-header %q — use X-Org-Id (%s) or X-Cloud-Org-Id (%s)",
		h, orgHeaders["X-Org-Id"], orgHeaders["X-Cloud-Org-Id"])
}

// mergeRules combines repeated inline flags with a rules file.
func mergeRules(inline stringList, path, kind string) ([]string, error) {
	rules := append([]string(nil), inline...)
	if strings.TrimSpace(path) == "" {
		return rules, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--%s-file %s: %w", kind, path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rules = append(rules, line)
	}
	return rules, nil
}

// ---------------------------------------------------------------------------
// rag wiki
// ---------------------------------------------------------------------------

func cmdWiki(args []string) error {
	if len(args) == 0 {
		printWikiUsage()
		return errors.New("wiki: a subcommand is required")
	}

	switch args[0] {
	case "probe":
		return cmdWikiProbe(args[1:])
	case "-h", "--help", "help":
		printWikiUsage()
		return nil
	default:
		printWikiUsage()
		return fmt.Errorf("unknown wiki subcommand %q", args[0])
	}
}

func printWikiUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  rag wiki probe --slug <slug> --org-id <id> [options]

probe issues the three requests the crawler depends on and prints the SHAPE of
each response — key names, JSON types and string lengths, never values — plus a
verdict on which fields internal/source/wiki/dto.go managed to recognise.

Run it first, with a real token: the field names in dto.go are informed guesses
until a live response confirms them.

The token is not a flag. Set it in the environment:
  export %s='<user account OAuth token>'

Run 'rag wiki probe --help' for the full flag list.
`, envWikiToken)
}

// cmdWikiProbe implements `rag wiki probe`.
func cmdWikiProbe(args []string) error {
	fs := flag.NewFlagSet("wiki probe", flag.ExitOnError)
	slug := fs.String("slug", "", "wiki page slug to inspect (required)")
	apiURL := fs.String("api-url", wiki.DefaultAPIURL, "wiki API base URL")
	orgID := fs.String("org-id", "", "organization id (required)")
	orgHeader := fs.String("org-header", "X-Org-Id", "header carrying the org id: X-Org-Id | X-Cloud-Org-Id")
	tokenFile := fs.String("token-file", "", "file holding the OAuth token; must be chmod 600. Prefer the "+envWikiToken+" env var")
	dumpDir := fs.String("dump-dir", "", "also write this report to a 0600 file in this directory")
	timeout := fs.Duration("timeout", 30*time.Second, "per-request timeout")
	_ = fs.Parse(args)

	if strings.TrimSpace(*slug) == "" {
		return errors.New("--slug is required: name one page to inspect, e.g. --slug docs/public/faq")
	}
	if err := validateOrgHeader(*orgHeader); err != nil {
		return err
	}
	if strings.TrimSpace(*orgID) == "" {
		return fmt.Errorf("--org-id is required: the API resolves the organization from the %s header, "+
			"and without it every request comes back 403.\n"+
			"  Yandex 360 for Business uses --org-header X-Org-Id; Yandex Cloud Organization uses --org-header X-Cloud-Org-Id",
			*orgHeader)
	}

	// The token is read last of the local checks, so a plain typo never sends a
	// user hunting for credentials.
	token, err := resolveWikiToken(*tokenFile)
	if err != nil {
		return err
	}

	client, err := wiki.NewClient(wiki.ClientConfig{
		APIURL:      *apiURL,
		Token:       token,
		OrgID:       *orgID,
		OrgHeader:   *orgHeader,
		Timeout:     *timeout,
		MaxRetries:  2,
		MinInterval: 100 * time.Millisecond,
		// A probe is three requests. The budget is a guard against a retry storm
		// against a production API, not a real constraint.
		MaxRequests: 20,
	})
	if err != nil {
		return err
	}

	out := io.Writer(os.Stdout)
	if strings.TrimSpace(*dumpDir) != "" {
		f, path, err := createDumpFile(*dumpDir, *slug)
		if err != nil {
			return err
		}
		defer f.Close()
		fmt.Fprintf(os.Stderr, "writing a copy of this report to %s\n", path)
		out = io.MultiWriter(os.Stdout, f)
	}

	ctx := context.Background()

	// Preflight: one request, so a broken token, a wrong org header or an
	// unreachable host produces ONE clear sentence instead of three consecutive
	// "REQUEST FAILED" blocks inside the report.
	if err := probePreflight(ctx, client, *slug, *orgHeader); err != nil {
		return err
	}

	return wiki.NewProbe(client, out).Run(ctx, *slug)
}

// probePreflight verifies that the API is reachable and the credentials work.
func probePreflight(ctx context.Context, c *wiki.Client, slug, orgHeader string) error {
	_, err := c.GetJSON(ctx, "/v1/pages", url.Values{"slug": {slug}})
	if err == nil {
		return nil
	}
	return diagnoseWikiErr(err, slug, orgHeader)
}

// diagnoseWikiErr turns a transport or API failure into an explanation with a
// next step. APIError already carries the 401/403 account advice; this adds the
// parts that depend on how the command was invoked.
func diagnoseWikiErr(err error, slug, orgHeader string) error {
	var apiErr *wiki.APIError
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("wiki request failed: %w", err)
	}

	switch {
	case apiErr.Status == 0:
		return fmt.Errorf(`cannot reach the wiki API: %w

  Nothing was authenticated — this failed at the network layer.
  Check that the host is reachable from here (corporate VPN or proxy may be
  required), and that --api-url is right; the default is %s.`, err, wiki.DefaultAPIURL)

	case apiErr.Status == 401:
		return fmt.Errorf(`%w

  The token was rejected. Check that %s holds a current OAuth token issued for a
  USER account — service accounts cannot authorize against this API.`, err, envWikiToken)

	case apiErr.Status == 403:
		other := "X-Cloud-Org-Id"
		if orgHeader == other {
			other = "X-Org-Id"
		}
		return fmt.Errorf(`%w

  The token is valid but this request was refused. Two usual causes:
    1. wrong org header — you sent %s; try --org-header %s
    2. this account cannot read %q`, err, orgHeader, other, slug)

	case apiErr.Status == 404:
		return fmt.Errorf(`%w

  No page at slug %q. Check the slug as it appears in the page URL, without the
  leading slash and without the wiki domain.`, err, slug)

	case apiErr.Status == 429:
		return fmt.Errorf(`%w

  The API is rate limiting this account. Retry later; for a full crawl lower the
  request rate with --max-requests.`, err)
	}

	return err
}

// createDumpFile opens a 0600 report file inside a 0700 directory. The probe's
// output is structural (key names, types, lengths) and already redacted, but the
// directory and file are still owner-only: it describes the layout of an
// internal wiki, and that is not world-readable material either.
func createDumpFile(dir, slug string) (*os.File, string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", fmt.Errorf("--dump-dir %s: %w", dir, err)
	}
	name := fmt.Sprintf("probe-%s-%s.txt", sanitizeForFilename(slug), time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(dir, name)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("--dump-dir %s: %w", dir, err)
	}
	if err := f.Chmod(0o600); err != nil { // O_TRUNC on an existing file keeps its old mode
		f.Close()
		return nil, "", fmt.Errorf("--dump-dir %s: %w", dir, err)
	}
	return f, path, nil
}

// sanitizeForFilename reduces a slug to something safe to place in a path: no
// separators, no traversal, bounded length.
func sanitizeForFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "page"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

// allowFlagsFor renders the --allow flags that would keep everything under the
// given roots, for use in the fail-closed error message. Allowing the roots is
// the common intent, and spelling out the exact flags saves the user a guess
// about how a root maps onto a prefix.
func allowFlagsFor(roots []string) string {
	// Normalised, so the suggested rule is character-for-character what the
	// filter will use and what the manifest will record. Echoing the raw root —
	// real ones end in "/" — would hand back a rule that works but never appears
	// anywhere in that form, which is a small lie in a message whose whole job is
	// to be copied verbatim.
	kept, _ := wiki.NormalizeRules(roots)
	parts := make([]string, 0, len(kept))
	for _, r := range kept {
		parts = append(parts, "--allow "+r)
	}
	if len(parts) == 0 {
		return "--allow <slug prefix>"
	}
	return strings.Join(parts, " ")
}
