package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rag/internal/chunker"
	"rag/internal/domain"
	"rag/internal/embedder"
	"rag/internal/source"
	"rag/internal/source/file"
	"rag/internal/source/wiki"
	"rag/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "index":
		err = cmdIndex(os.Args[2:])
	case "search":
		err = cmdSearch(os.Args[2:])
	case "stats":
		err = cmdStats(os.Args[2:])
	case "wiki":
		err = cmdWiki(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  rag index  --input <path> [options]        — index local files into SQLite
  rag index  --source wiki --root <slug> …   — index a Yandex 360 wiki subtree
  rag search --query <text> [options]        — semantic search
  rag stats  --db <path>                     — show index statistics
  rag wiki   probe --slug <slug> [options]   — inspect the live wiki API responses

The wiki OAuth token is NEVER a flag. Provide it as:
  export WIKI_OAUTH_TOKEN=<token>      (a USER account token; service accounts are unsupported)
or via --token-file <path>, which must not be readable by group or other (chmod 600).

Recommended order for a wiki index:
  1. rag wiki probe   — learn the real API field names, fix internal/source/wiki/dto.go
  2. rag index --source wiki --dry-run …  — produce a manifest
  3. review the manifest by hand
  4. rag index --source wiki …            — the real run

Run 'rag <command> --help' for per-command flags.`)
}

// ---------------------------------------------------------------------------
// index
// ---------------------------------------------------------------------------

func cmdIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)

	sourceKind := fs.String("source", "file", "document source: file|wiki")
	input := fs.String("input", "", "file or directory to index (required for --source file)")
	dbPath := fs.String("db", "rag.db", "SQLite database path")
	strategy := fs.String("strategy", "fixed", "chunking strategy: fixed|structural")
	chunkSize := fs.Int("chunk-size", 512, "max chunk size in characters")
	overlap := fs.Int("overlap", 64, "overlap between chunks in characters")
	embedURL := fs.String("embed-url", "http://localhost:11434", "embeddings API base URL")
	embedModel := fs.String("embed-model", "nomic-embed-text", "embeddings model name")
	embedKey := fs.String("embed-key", "", "API key (leave empty for local LLMs)")

	force := fs.Bool("force", false, "reindex every document, ignoring the stored version/hash")
	dryRun := fs.Bool("dry-run", false, "walk, filter, check ACLs and fetch content, but never embed and never touch the database")
	manifestPath := fs.String("manifest", "", "manifest path (default: <db> with a .manifest.json suffix)")

	wf := addWikiIndexFlags(fs)
	_ = fs.Parse(args)

	ctx := context.Background()

	// PrefixHeading is enabled for wiki only: it changes the embedded text, so
	// switching it on for the file source would silently invalidate every
	// existing file-built database until it is fully reindexed.
	ch, err := chunker.New(*strategy, chunker.Config{
		ChunkSize:     *chunkSize,
		Overlap:       *overlap,
		PrefixHeading: *sourceKind == "wiki",
	})
	if err != nil {
		return err
	}

	var (
		src      source.Source
		manifest *wiki.Manifest
		mPath    string
	)

	switch *sourceKind {
	case "file":
		if *input == "" {
			return errors.New("--input is required for --source file")
		}
		src = file.New(*input)

	case "wiki":
		ws, err := buildWikiSource(wf, *dryRun, os.Stderr)
		if err != nil {
			return err
		}
		src = ws
		manifest = ws.Manifest()
		mPath = resolveManifestPath(*manifestPath, *dbPath)

	default:
		return fmt.Errorf("unknown --source %q — use file or wiki", *sourceKind)
	}

	// The manifest is written on EVERY exit path, including a failed crawl: a
	// partial manifest still documents how far the run got and why it stopped,
	// which is exactly what a human needs in order to retry deliberately.
	writeManifest := func() error {
		if manifest == nil {
			return nil
		}
		if err := manifest.WriteJSON(mPath); err != nil {
			return err
		}
		table := strings.TrimSuffix(mPath, filepath.Ext(mPath)) + ".txt"
		if err := manifest.WriteTableFile(table); err != nil {
			return err
		}
		fmt.Printf("\nmanifest: %s\n          %s\n", mPath, table)
		return nil
	}

	if *dryRun {
		return runDryRun(ctx, src, manifest, writeManifest)
	}

	emb := embedder.New(*embedURL, *embedModel, *embedKey)

	var stats indexStats
	runErr := withAtomicStore(*dbPath, *force, func(st *store.Store) error {
		if err := indexSource(ctx, src, ch, emb, st, *force, &stats); err != nil {
			return err
		}
		return writeIndexMeta(ctx, st, *embedModel, *sourceKind, mPath)
	})

	mErr := writeManifest()
	if runErr != nil {
		return runErr
	}
	if mErr != nil {
		return mErr
	}

	fmt.Printf("\nindexed: %d document(s), %d chunk(s); unchanged: %d\ndatabase: %s\n",
		stats.Indexed, stats.Chunks, stats.Skipped, *dbPath)
	if manifest != nil {
		printManifestSummary(manifest)
	}
	return nil
}

// runDryRun performs the full walk without an embedder and without ever opening
// a database. Nothing is written to the index — the point of a dry run is that
// the only artefact is a manifest a human can review before committing to a real
// crawl of a corporate wiki.
func runDryRun(ctx context.Context, src source.Source, manifest *wiki.Manifest, writeManifest func() error) error {
	var docs int
	runErr := src.Iterate(ctx, func(doc source.Document) error {
		// The wiki source never calls fn in dry-run mode; the file source does,
		// so count and report without keeping any content.
		docs++
		label := doc.Path
		if label == "" {
			label = doc.ID
		}
		fmt.Printf("would index %s (%d bytes)\n", label, len(doc.Content))
		return nil
	})

	mErr := writeManifest()

	fmt.Println("\nDRY RUN — no embeddings were requested and no database was written.")
	if manifest != nil {
		printManifestSummary(manifest)
	} else {
		fmt.Printf("%d document(s) would be indexed\n", docs)
	}

	if runErr != nil {
		return runErr
	}
	return mErr
}

// printManifestSummary renders the include/exclude tally with a per-reason
// breakdown. A raw "12 pages skipped" is not reviewable; "8 deny_prefix,
// 4 acl_unknown" tells a person whether the filter or the ACL contract is wrong.
func printManifestSummary(m *wiki.Manifest) {
	included := m.Indexed()
	total := len(m.Entries)
	fmt.Printf("\n%d included / %d excluded (of %d page(s) visited)\n", included, total-included, total)
	for _, line := range sortedSummary(m.Summary()) {
		fmt.Printf("  %s\n", line)
	}
	printEmptyResultDiagnosis(m, included, total)
	// The permission warning is printed LAST, so it is the final thing on screen,
	// and on every run — dry or real. A control that compensates for the absence
	// of a permission check is worthless if it only appears in the rehearsal.
	if warn := m.ACLWarning(); warn != "" {
		fmt.Print(warn)
	}
}

// printEmptyResultDiagnosis explains a run that walked real pages and kept none
// of them.
//
// This case looks like success — it exits 0, writes a manifest, and reports its
// tally in the same calm voice as a good run — while producing an index that
// answers every support question with "I don't know". The crawl already proved
// the token, the org id and the roots are right, so the fault is almost always a
// filter rule that does not match the shape of a real slug. The rules and the
// slugs are both known here; printing them side by side is the whole diagnosis.
func printEmptyResultDiagnosis(m *wiki.Manifest, included, total int) {
	if included > 0 || total == 0 {
		return
	}

	fmt.Println("\n  NOTHING WAS INDEXED, although the crawl did reach real pages.")
	fmt.Println("  The token, the org id and the roots are therefore fine.")

	// Blame the stage that actually rejected the pages. An earlier cut of this
	// diagnosis always accused the filter, which would have sent someone editing
	// correct --allow rules while the real refusal came from the ACL check.
	if reason, n := dominantSkipReason(m); n == total && strings.HasPrefix(reason, "acl_") {
		printACLSkipDiagnosis(reason, n)
		return
	}

	fmt.Println("  The filter is what rejected them.")
	fmt.Println("\n  Allow rules used (after normalisation):")
	if len(m.Allow) == 0 {
		fmt.Println("    (none — fail-closed, so every page is excluded)")
	}
	for _, r := range m.Allow {
		fmt.Printf("    --allow %s\n", r)
	}
	if len(m.Deny) > 0 {
		fmt.Println("  Deny rules used:")
		for _, r := range m.Deny {
			fmt.Printf("    --deny %s\n", r)
		}
	}

	samples := m.SampleSlugs(3)
	if len(samples) > 0 {
		fmt.Println("\n  Slugs actually returned by the API:")
		for _, s := range samples {
			fmt.Printf("    %s\n", s)
		}
		fmt.Println("\n  An allow rule must be one of these slugs or a leading path segment of one.")
		// Suggest the common prefix of everything visited, not the first slug: a
		// leaf slug would keep exactly one page, which looks like a fix and is
		// not one. The prefix is derived from what the API returned rather than
		// from what was typed, so it is right even when the typed root was.
		if prefix := commonSlugPrefix(m.SampleSlugs(0)); prefix != "" {
			fmt.Printf("  To keep everything the crawl found:\n    --allow %s\n", prefix)
		}
	}
	fmt.Println("\n  Matching is segment-wise: \"docs\" matches \"docs/api\" but not \"docsecret\".")
	fmt.Println("  A full page URL is not a slug — paste the path only, without the host.")
}

// dominantSkipReason returns the reason that accounts for the most entries, and
// how many. The reason recorded in an entry may carry a ":rule" suffix; only the
// bare reason is returned, since that is what names the stage.
func dominantSkipReason(m *wiki.Manifest) (string, int) {
	counts := map[string]int{}
	for _, e := range m.Entries {
		if e.Decision == wiki.DecisionIndexed {
			continue
		}
		reason, _, _ := strings.Cut(e.Reason, ":")
		counts[reason]++
	}
	var best string
	var bestN int
	for r, n := range counts {
		if n > bestN || (n == bestN && r < best) {
			best, bestN = r, n
		}
	}
	return best, bestN
}

// printACLSkipDiagnosis explains a crawl that every page failed the permission
// check on.
//
// This is the expected outcome against the live Yandex 360 wiki, whose /access
// endpoint answers 405 — and it is a CONFIGURATION result, not a fault: it means
// the run is in a mode that refuses to index what it cannot vet. Saying so is
// the point. The wrong reaction is to reach for --acl-mode=off, which is why the
// consequence of each mode is spelled out rather than the flag merely named.
func printACLSkipDiagnosis(reason string, n int) {
	fmt.Printf("  Every one of the %d page(s) was rejected by the PERMISSION check (%s), not by the filter.\n", n, reason)
	fmt.Println("\n  This API has no working page-permission endpoint: /access answers 405.")
	fmt.Println("  So the check cannot succeed, and the current --acl-mode refuses to index unvetted pages.")
	fmt.Println("\n  The choice is a policy one:")
	fmt.Println("    --acl-mode=probe    index anyway, marking every page acl=unchecked, and warn loudly")
	fmt.Println("                        on every run. This is the default.")
	fmt.Println("    --acl-mode=require  what you have now: index nothing that cannot be vetted.")
	fmt.Println("    --acl-mode=off      skip the check entirely and spend no requests on it.")
	fmt.Println("\n  Under probe and off, the ONLY barriers between a restricted page and whoever")
	fmt.Println("  queries this index are the --allow/--deny rules and a human reading the manifest.")
	fmt.Println("  Read the slug list before publishing the index, not after.")
}

// commonSlugPrefix returns the longest leading path SEGMENT sequence shared by
// every slug. Segment-wise, because the filter matches that way: the character
// prefix of "docs/api" and "docs/apiary" is "docs/api", which as a rule would
// silently also match a third section nobody looked at.
func commonSlugPrefix(slugs []string) string {
	if len(slugs) == 0 {
		return ""
	}
	common := strings.Split(slugs[0], "/")
	for _, s := range slugs[1:] {
		parts := strings.Split(s, "/")
		if len(parts) < len(common) {
			common = common[:len(parts)]
		}
		for i := range common {
			if parts[i] != common[i] {
				common = common[:i]
				break
			}
		}
		if len(common) == 0 {
			return ""
		}
	}
	return strings.Join(common, "/")
}

func sortedSummary(sum map[string]int) []string {
	keys := make([]string, 0, len(sum))
	for k := range sum {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%-40s %d", k, sum[k]))
	}
	return out
}

// indexStats accumulates what a run did, for the closing line.
type indexStats struct {
	Indexed int
	Skipped int
	Chunks  int
}

// indexSource runs the shared indexing pipeline over any source: skip documents
// whose stored revision still matches, otherwise chunk, embed and replace the
// document's chunks in the store.
func indexSource(ctx context.Context, src source.Source, ch chunker.Chunker, emb *embedder.Client, st *store.Store, force bool, stats *indexStats) error {
	return src.Iterate(ctx, func(doc source.Document) error {
		label := doc.Path
		if label == "" {
			label = doc.ID
		}

		hash := contentHash(doc.Content)

		if !force {
			state, found, err := st.DocumentState(ctx, doc.ID)
			if err != nil {
				return fmt.Errorf("document state of %s: %w", label, err)
			}
			if found && unchanged(state, doc, hash) {
				stats.Skipped++
				fmt.Printf("unchanged %s (%d chunks)\n", label, state.ChunkCount)
				return nil
			}
		}

		fmt.Printf("indexing %s\n", label)

		chunks := ch.Chunk(doc.Content, domain.ChunkMeta{
			Source: doc.ID,
			File:   doc.Title,
			Title:  doc.Title,
			URL:    doc.URL,
			Format: doc.Format,
		})
		fmt.Printf("  → %d chunks\n", len(chunks))

		// Drop the previous revision first: SaveChunk is an upsert keyed by chunk
		// id, so a document that now yields fewer chunks would otherwise keep its
		// stale tail in the index.
		if err := st.DeleteChunksBySource(ctx, doc.ID); err != nil {
			return fmt.Errorf("delete chunks of %s: %w", label, err)
		}

		for i, c := range chunks {
			vec, err := emb.Embed(ctx, c.Content)
			if err != nil {
				return fmt.Errorf("embed chunk %d of %s: %w", i, label, err)
			}
			if err := st.SaveChunk(ctx, c, vec); err != nil {
				return fmt.Errorf("save chunk %d of %s: %w", i, label, err)
			}
		}

		stats.Indexed++
		stats.Chunks += len(chunks)

		return st.SaveDocument(ctx, store.DocumentState{
			Source:      doc.ID,
			URL:         doc.URL,
			Title:       doc.Title,
			Version:     doc.Version,
			ContentHash: hash,
			ChunkCount:  len(chunks),
		})
	})
}

// unchanged decides whether a document can be skipped. The content hash is
// authoritative when the index has one; the source's own version marker is the
// fallback for rows written before hashes were recorded.
func unchanged(state store.DocumentState, doc source.Document, hash string) bool {
	if state.ContentHash != "" {
		return state.ContentHash == hash
	}
	return doc.Version != "" && state.Version == doc.Version
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// writeIndexMeta records what this index is, so a consumer can tell whether the
// database matches the embedding model it is about to query with — a mismatch
// produces plausible-looking nonsense rather than an error.
func writeIndexMeta(ctx context.Context, st *store.Store, embedModel, sourceKind, manifestPath string) error {
	meta := [][2]string{
		{"embed_model", embedModel},
		{"source_kind", sourceKind},
		{"indexed_at", time.Now().UTC().Format(time.RFC3339)},
		{"manifest_path", manifestPath},
	}
	for _, kv := range meta {
		if err := st.SetMeta(ctx, kv[0], kv[1]); err != nil {
			return fmt.Errorf("index_meta %s: %w", kv[0], err)
		}
	}
	return nil
}

func resolveManifestPath(explicit, dbPath string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	base := strings.TrimSuffix(dbPath, filepath.Ext(dbPath))
	if base == "" {
		base = "rag"
	}
	return base + ".manifest.json"
}

// ---------------------------------------------------------------------------
// atomic database swap
// ---------------------------------------------------------------------------

// withAtomicStore runs fn against a temporary database in the same directory as
// dbPath and only replaces the live database once fn has fully succeeded.
//
// The reason is not tidiness. A half-written index is indistinguishable from a
// complete one at query time: the bot answers confidently and is simply missing
// pages, which nobody notices for weeks. An outright failure that leaves the old
// index in place is strictly better than that.
func withAtomicStore(dbPath string, force bool, fn func(*store.Store) error) (err error) {
	dir := filepath.Dir(dbPath)
	tmp := filepath.Join(dir, fmt.Sprintf("%s.tmp-%d", filepath.Base(dbPath), os.Getpid()))
	removeDB(tmp)

	// Incremental runs start from a copy of the live index so that unchanged
	// documents can be skipped; --force starts from an empty database, which is
	// also how a stale document that no longer exists at the source gets dropped.
	if !force {
		if err := copyIfExists(dbPath, tmp); err != nil {
			return fmt.Errorf("copy %s for atomic update: %w", dbPath, err)
		}
	}

	st, err := store.Open(tmp)
	if err != nil {
		removeDB(tmp)
		return fmt.Errorf("open store: %w", err)
	}

	defer func() {
		if r := recover(); r != nil {
			st.Close()
			removeDB(tmp)
			panic(r)
		}
	}()

	if err := fn(st); err != nil {
		st.Close()
		removeDB(tmp)
		return err
	}
	if err := st.Close(); err != nil {
		removeDB(tmp)
		return fmt.Errorf("close temporary store: %w", err)
	}

	if err := os.Rename(tmp, dbPath); err != nil {
		removeDB(tmp)
		return fmt.Errorf("replace %s: %w", dbPath, err)
	}
	return nil
}

// removeDB deletes a SQLite database and any journal/WAL siblings it may have
// left behind, so a failed run leaves no debris next to the live index.
func removeDB(path string) {
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}

func copyIfExists(src, dst string) error {
	in, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	query := fs.String("query", "", "search query text (required)")
	dbPath := fs.String("db", "rag.db", "SQLite database path")
	topK := fs.Int("top-k", 5, "number of results to return")
	embedURL := fs.String("embed-url", "http://localhost:11434", "embeddings API base URL")
	embedModel := fs.String("embed-model", "nomic-embed-text", "embeddings model name")
	embedKey := fs.String("embed-key", "", "API key (leave empty for local LLMs)")
	_ = fs.Parse(args)

	if *query == "" {
		return fmt.Errorf("--query is required")
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	emb := embedder.New(*embedURL, *embedModel, *embedKey)

	vec, err := emb.Embed(context.Background(), *query)
	if err != nil {
		return fmt.Errorf("embed query: %w", err)
	}

	results, err := st.Search(context.Background(), vec, *topK)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("no results found")
		return nil
	}

	for i, r := range results {
		fmt.Printf("\n─── Result %d  (similarity %.4f) ───\n", i+1, r.Similarity)
		fmt.Printf("Source:   %s\n", r.Chunk.Meta.Source)
		fmt.Printf("File:     %s\n", r.Chunk.Meta.File)
		if r.Chunk.Meta.Title != "" {
			fmt.Printf("Title:    %s\n", r.Chunk.Meta.Title)
		}
		// The URL is what a support answer cites. Databases built before URLs
		// were stored have none, so the line is omitted rather than printed empty.
		if r.Chunk.Meta.URL != "" {
			fmt.Printf("URL:      %s\n", r.Chunk.Meta.URL)
		}
		if r.Chunk.Meta.Section != "" {
			fmt.Printf("Section:  %s\n", r.Chunk.Meta.Section)
		}
		fmt.Printf("ChunkID:  %d\n", r.Chunk.Meta.ChunkID)
		fmt.Printf("\n%s\n", r.Chunk.Content)
	}
	return nil
}

// ---------------------------------------------------------------------------
// stats
// ---------------------------------------------------------------------------

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	dbPath := fs.String("db", "rag.db", "SQLite database path")
	_ = fs.Parse(args)

	st, err := store.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()

	total, embedded, err := st.Stats(ctx)
	if err != nil {
		return err
	}
	docs, err := st.DocumentCount(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Documents:       %d\n", docs)
	fmt.Printf("Chunks total:    %d\n", total)
	fmt.Printf("Chunks embedded: %d\n", embedded)
	fmt.Printf("Chunks pending:  %d\n", total-embedded)

	fmt.Println("\nIndex metadata:")
	var any bool
	for _, key := range []string{"source_kind", "embed_model", "indexed_at", "manifest_path"} {
		value, ok, err := st.GetMeta(ctx, key)
		if err != nil {
			return err
		}
		if !ok || value == "" {
			continue
		}
		any = true
		fmt.Printf("  %-14s %s\n", key+":", value)
	}
	if !any {
		fmt.Println("  (none — this database predates index_meta, or was never written by `rag index`)")
	}
	return nil
}
