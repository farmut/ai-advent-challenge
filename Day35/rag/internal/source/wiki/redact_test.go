package wiki

import (
	"net/http"
	"strings"
	"testing"
)

const canary = "TOKEN-CANARY-y0_AgAAAABxxxxxxxAAxxxx"

func TestRedactRegisteredSecret(t *testing.T) {
	resetSecrets()
	t.Cleanup(resetSecrets)
	RegisterSecret(canary)

	tests := []string{
		canary,
		"token is " + canary + " ok",
		`{"echo":"` + canary + `"}`,
		"https://api.wiki.yandex.net/v1/pages?x=" + canary,
	}
	for _, in := range tests {
		got := redact(in)
		if strings.Contains(got, canary) {
			t.Errorf("redact(%q) = %q — the token survived", in, got)
		}
		if !strings.Contains(got, mask) {
			t.Errorf("redact(%q) = %q — no mask marker", in, got)
		}
	}
}

func TestRedactAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"header line", "Authorization: OAuth abcdef123456"},
		{"json field", `{"Authorization":"OAuth abcdef123456"}`},
		{"lowercase", "authorization: oauth abcdef123456"},
		{"equals", "Authorization=abcdef123456"},
		{"bearer", "Authorization: Bearer abcdef123456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redact(tt.in)
			if strings.Contains(got, "abcdef123456") {
				t.Errorf("redact(%q) = %q — the credential survived", tt.in, got)
			}
		})
	}
}

func TestRedactOAuthAndBearerAnywhere(t *testing.T) {
	// Even without registration, the scheme prefix gives the secret away.
	resetSecrets()
	t.Cleanup(resetSecrets)

	tests := []struct{ in, keep, drop string }{
		{"OAuth y0_AgAAAAsecret", "OAuth", "y0_AgAAAAsecret"},
		{"oauth y0_AgAAAAsecret", "oauth", "y0_AgAAAAsecret"},
		{"Bearer eyJhbGciOiJIUzI1", "Bearer", "eyJhbGciOiJIUzI1"},
		{"request used OAuth  y0_secret_value here", "OAuth", "y0_secret_value"},
	}

	for _, tt := range tests {
		got := redact(tt.in)
		if strings.Contains(got, tt.drop) {
			t.Errorf("redact(%q) = %q — secret %q survived", tt.in, got, tt.drop)
		}
		if !strings.Contains(got, tt.keep) {
			t.Errorf("redact(%q) = %q — dropped the scheme name %q, which is not secret and aids debugging",
				tt.in, got, tt.keep)
		}
	}
}

func TestRedactQueryParameters(t *testing.T) {
	resetSecrets()
	t.Cleanup(resetSecrets)

	tests := []struct{ in, drop string }{
		{"https://host/v1/pages?access_token=SECRETVALUE123&slug=docs", "SECRETVALUE123"},
		{"https://host/v1/pages?oauth_token=SECRETVALUE123", "SECRETVALUE123"},
		{"https://host/v1/pages?token=SECRETVALUE123&x=1", "SECRETVALUE123"},
		{"?access_token=SECRETVALUE123", "SECRETVALUE123"},
	}

	for _, tt := range tests {
		got := redact(tt.in)
		if strings.Contains(got, tt.drop) {
			t.Errorf("redact(%q) = %q — query secret survived", tt.in, got)
		}
	}

	// A non-secret parameter must survive: over-redaction would make URLs in the
	// manifest useless.
	if got := redact("https://host/v1/pages?slug=docs/api"); !strings.Contains(got, "slug=docs/api") {
		t.Errorf("redact stripped a harmless query parameter: %q", got)
	}
}

// The API echoes the request back inside debug_message. That is the single most
// likely way a token reaches a log file.
func TestAPIErrorRedactsDebugMessageEcho(t *testing.T) {
	resetSecrets()
	t.Cleanup(resetSecrets)
	RegisterSecret(canary)

	e := &APIError{
		Status:       http.StatusBadRequest,
		ErrorCode:    "bad_request",
		DebugMessage: "failed request with header Authorization: OAuth " + canary,
		Details:      `{"sent":"` + canary + `"}`,
	}

	msg := e.Error()
	if strings.Contains(msg, canary) {
		t.Errorf("APIError.Error() leaked the token:\n%s", msg)
	}
	if !strings.Contains(msg, "bad_request") {
		t.Errorf("APIError.Error() lost the error code:\n%s", msg)
	}
}

// A 401 must tell the user the actual cause: service accounts do not work here.
func TestAPIError401MentionsUserAccount(t *testing.T) {
	msg := (&APIError{Status: http.StatusUnauthorized}).Error()

	for _, want := range []string{"USER account", "service accounts are not supported", "WIKI_OAUTH_TOKEN"} {
		if !strings.Contains(msg, want) {
			t.Errorf("401 message does not mention %q:\n%s", want, msg)
		}
	}
}

func TestRegisterSecretIgnoresShortValues(t *testing.T) {
	resetSecrets()
	t.Cleanup(resetSecrets)
	RegisterSecret("abc") // too short to mask safely

	if got := redact("abc is a common substring"); got != "abc is a common substring" {
		t.Errorf("a 3-char secret corrupted unrelated text: %q", got)
	}
}

func TestRedactEmptyString(t *testing.T) {
	if got := redact(""); got != "" {
		t.Errorf("redact(\"\") = %q", got)
	}
}
