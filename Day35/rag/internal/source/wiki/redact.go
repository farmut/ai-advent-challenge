package wiki

import (
	"regexp"
	"strings"
	"sync"
)

// mask is what replaces every secret. It is deliberately distinctive so that a
// leak test can assert on its presence and a human can spot it in a manifest.
const mask = "[REDACTED]"

// Patterns that hide a secret regardless of where the string came from.
// They are intentionally broad: over-masking a manifest is harmless, leaking an
// OAuth token into a log file is not.
var (
	// "OAuth <token>" / "Bearer <token>" — the Authorization header value, which
	// APIs love to echo back inside debug_message.
	reAuthScheme = regexp.MustCompile(`(?i)\b(OAuth|Bearer)\s+\S+`)
	// Authorization header rendered as a header line or as a map entry:
	//   Authorization: OAuth abc / "Authorization":"OAuth abc" / Authorization=abc
	reAuthHeader = regexp.MustCompile(`(?i)"?Authorization"?\s*[:=]\s*"?[^"\r\n,}]*"?`)
	// Secret-bearing query parameters. Matches inside a URL or a bare query string.
	reQuerySecret = regexp.MustCompile(`(?i)\b(access_token|oauth_token|token)\s*=\s*[^&\s"'\\]*`)
)

// secrets holds the literal values registered via RegisterSecret. Literal
// substring masking is the only defence against a token that appears in a
// context none of the patterns anticipated (a JSON field we have never seen, a
// URL-encoded copy, a stack trace).
var secrets struct {
	sync.RWMutex
	values []string
}

// RegisterSecret marks a literal value — the OAuth token, the org id if it is
// considered sensitive — so that redact() masks it anywhere it appears.
// Values shorter than 8 characters are ignored: masking a short string would
// corrupt unrelated text (a token "abc" would eat every "abc" in the output).
func RegisterSecret(value string) {
	value = strings.TrimSpace(value)
	if len(value) < 8 {
		return
	}
	secrets.Lock()
	defer secrets.Unlock()
	for _, v := range secrets.values {
		if v == value {
			return
		}
	}
	secrets.values = append(secrets.values, value)
}

// resetSecrets clears the registry. Tests only.
func resetSecrets() {
	secrets.Lock()
	defer secrets.Unlock()
	secrets.values = nil
}

// redact masks every known secret in s. Everything the package writes to
// stderr, to a manifest, to a dump file or into an error message goes through
// it. Order matters: literal values first (they are exact and cheap), then the
// structural patterns, so a token already masked cannot resurface.
func redact(s string) string {
	if s == "" {
		return s
	}

	secrets.RLock()
	values := secrets.values
	secrets.RUnlock()
	for _, v := range values {
		s = strings.ReplaceAll(s, v, mask)
	}

	s = reAuthHeader.ReplaceAllString(s, "Authorization: "+mask)
	s = reAuthScheme.ReplaceAllStringFunc(s, func(m string) string {
		scheme := m[:strings.IndexAny(m, " \t")]
		return scheme + " " + mask
	})
	s = reQuerySecret.ReplaceAllStringFunc(s, func(m string) string {
		return m[:strings.Index(m, "=")+1] + mask
	})

	return s
}
