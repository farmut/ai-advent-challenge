package wiki

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Case 3: a 429 carrying Retry-After: 1 must produce exactly one retry, honour
// the server's delay over our own backoff, and then succeed.
func TestClientRetriesOn429WithRetryAfter(t *testing.T) {
	f := newFakeWiki(t)

	var calls int
	f.page = func(slug, id string) (int, string, map[string]string) {
		calls++
		if calls == 1 {
			return http.StatusTooManyRequests, `{"error_code":"rate_limited"}`, map[string]string{"Retry-After": "1"}
		}
		return http.StatusOK, pageJSON("1", "docs", "Docs", longBody), nil
	}

	c, sleeps := f.newTestClient(t, nil)

	raw, err := c.GetJSON(context.Background(), "/v1/pages", nil)
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if !strings.Contains(string(raw), "Docs") {
		t.Errorf("unexpected body: %s", raw)
	}

	if calls != 2 {
		t.Errorf("server saw %d requests, want 2 (one 429 + one success)", calls)
	}
	if got := c.Requests(); got != 2 {
		t.Errorf("client counted %d requests, want 2", got)
	}

	delays := sleeps.all()
	if len(delays) != 1 {
		t.Fatalf("slept %d time(s), want exactly 1: %v", len(delays), delays)
	}
	if delays[0] != time.Second {
		t.Errorf("delay = %v, want 1s from Retry-After (the server's hint must win over our backoff)", delays[0])
	}
}

// An absurd Retry-After must not park the crawl for hours. It is refused
// outright rather than silently shortened — shortening it would mean retrying
// sooner than the server permitted and making the throttling worse.
func TestClientRefusesExcessiveRetryAfter(t *testing.T) {
	f := newFakeWiki(t)

	var calls int
	f.page = func(slug, id string) (int, string, map[string]string) {
		calls++
		return http.StatusTooManyRequests, `{}`, map[string]string{"Retry-After": "86400"}
	}

	c, sleeps := f.newTestClient(t, func(cfg *ClientConfig) { cfg.MaxRetryAfter = time.Minute })

	_, err := c.GetJSON(context.Background(), "/v1/pages", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("server saw %d requests, want 1", calls)
	}
	if n := len(sleeps.all()); n != 0 {
		t.Errorf("slept %d time(s), want 0 — a 24h wait must be refused, not performed", n)
	}
	if !strings.Contains(err.Error(), "retry the crawl later") {
		t.Errorf("error does not explain what to do: %v", err)
	}
}

// Case 4: repeated 500s must back off exponentially and then give up, rather
// than retrying forever.
func TestClientGivesUpAfterMaxRetriesOn500(t *testing.T) {
	f := newFakeWiki(t)

	var calls int
	f.page = func(slug, id string) (int, string, map[string]string) {
		calls++
		return http.StatusInternalServerError, `{"error_code":"internal","debug_message":"boom"}`, nil
	}

	c, sleeps := f.newTestClient(t, func(cfg *ClientConfig) { cfg.MaxRetries = 3 })

	_, err := c.GetJSON(context.Background(), "/v1/pages", nil)
	if err == nil {
		t.Fatal("GetJSON succeeded against a permanently failing server")
	}

	// 1 initial attempt + 3 retries.
	if calls != 4 {
		t.Errorf("server saw %d requests, want 4 (initial + 3 retries)", calls)
	}
	if !strings.Contains(err.Error(), "giving up after 3 retries") {
		t.Errorf("error does not say it gave up: %v", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want to wrap *APIError", err)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", apiErr.Status)
	}

	delays := sleeps.all()
	if len(delays) != 3 {
		t.Fatalf("slept %d time(s), want 3: %v", len(delays), delays)
	}
	// Each delay is jittered into [d/2, d], so compare the ceilings.
	for i, d := range delays {
		if d <= 0 {
			t.Errorf("delay %d is %v, want positive", i, d)
		}
	}
	if delays[len(delays)-1] < delays[0] {
		t.Errorf("backoff did not grow: %v", delays)
	}
}

// Case 5: a 401 must not be retried — exactly one request — and the message must
// point at the real cause, which is almost always a service-account token.
func TestClient401IsNotRetriedAndExplainsUserAccount(t *testing.T) {
	f := newFakeWiki(t)

	var calls int
	f.page = func(slug, id string) (int, string, map[string]string) {
		calls++
		return http.StatusUnauthorized, `{"error_code":"unauthorized","debug_message":"invalid token"}`, nil
	}

	c, sleeps := f.newTestClient(t, func(cfg *ClientConfig) { cfg.MaxRetries = 5 })

	_, err := c.GetJSON(context.Background(), "/v1/pages", nil)
	if err == nil {
		t.Fatal("GetJSON succeeded against a 401")
	}

	if calls != 1 {
		t.Errorf("server saw %d requests, want EXACTLY 1 — retrying a 401 risks locking the account", calls)
	}
	if got := c.Requests(); got != 1 {
		t.Errorf("client counted %d requests, want 1", got)
	}
	if n := len(sleeps.all()); n != 0 {
		t.Errorf("slept %d time(s), want 0 — a 401 must fail immediately", n)
	}

	msg := err.Error()
	for _, want := range []string{"USER account", "service accounts are not supported", "WIKI_OAUTH_TOKEN"} {
		if !strings.Contains(msg, want) {
			t.Errorf("401 error does not mention %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "giving up after") {
		t.Errorf("401 was treated as retryable:\n%s", msg)
	}
}

func TestClientDoesNotRetryClientErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFakeWiki(t)
			var calls int
			f.page = func(slug, id string) (int, string, map[string]string) {
				calls++
				return status, `{"error_code":"nope"}`, nil
			}

			c, _ := f.newTestClient(t, func(cfg *ClientConfig) { cfg.MaxRetries = 5 })
			if _, err := c.GetJSON(context.Background(), "/v1/pages", nil); err == nil {
				t.Fatal("expected an error")
			}
			if calls != 1 {
				t.Errorf("server saw %d requests for %d, want 1", calls, status)
			}
		})
	}
}

// Case 8 (client half): MaxRequests is a hard circuit breaker on the whole run.
func TestClientMaxRequestsBudget(t *testing.T) {
	f := newFakeWiki(t)
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, pageJSON("1", "docs", "Docs", longBody), nil
	}

	c, _ := f.newTestClient(t, func(cfg *ClientConfig) { cfg.MaxRequests = 2 })

	for i := 0; i < 2; i++ {
		if _, err := c.GetJSON(context.Background(), "/v1/pages", nil); err != nil {
			t.Fatalf("request %d failed early: %v", i+1, err)
		}
	}

	_, err := c.GetJSON(context.Background(), "/v1/pages", nil)
	if !errors.Is(err, ErrRequestBudget) {
		t.Fatalf("error = %v, want ErrRequestBudget", err)
	}
	if got := f.count("/v1/pages"); got != 2 {
		t.Errorf("server saw %d requests, want 2 — the budget must be taken BEFORE the request", got)
	}
}

// A retry consumes the budget too: otherwise a retry storm escapes the limit.
func TestClientRetriesCountAgainstBudget(t *testing.T) {
	f := newFakeWiki(t)
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusInternalServerError, `{}`, nil
	}

	c, _ := f.newTestClient(t, func(cfg *ClientConfig) {
		cfg.MaxRetries = 10
		cfg.MaxRequests = 3
	})

	_, err := c.GetJSON(context.Background(), "/v1/pages", nil)
	if !errors.Is(err, ErrRequestBudget) {
		t.Fatalf("error = %v, want ErrRequestBudget", err)
	}
	if got := c.Requests(); got != 3 {
		t.Errorf("issued %d requests, want 3", got)
	}
}

func TestClientSendsAuthAndOrgHeaders(t *testing.T) {
	var gotAuth, gotOrg, gotAccept string

	f := newFakeWiki(t)
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, `{"id":"1","content":"x"}`, nil
	}
	// Wrap the server handler to capture headers.
	base := f.server.Config.Handler
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotOrg = r.Header.Get("X-Cloud-Org-Id")
		gotAccept = r.Header.Get("Accept")
		base.ServeHTTP(w, r)
	})

	c, _ := f.newTestClient(t, func(cfg *ClientConfig) {
		cfg.Token = "y0_secret_token_value"
		cfg.OrgID = "cloud-org-7"
		cfg.OrgHeader = "X-Cloud-Org-Id"
	})
	t.Cleanup(resetSecrets)

	if _, err := c.GetJSON(context.Background(), "/v1/pages", nil); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}

	if gotAuth != "OAuth y0_secret_token_value" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "OAuth y0_secret_token_value")
	}
	if gotOrg != "cloud-org-7" {
		t.Errorf("X-Cloud-Org-Id = %q, want %q", gotOrg, "cloud-org-7")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

// Constructing a client registers the token, so anything that later formats it
// into a message gets it masked without having to know about the config.
func TestNewClientRegistersTokenForRedaction(t *testing.T) {
	resetSecrets()
	t.Cleanup(resetSecrets)

	const tok = "y0_AgAAAAA_TEST_TOKEN_VALUE"
	if _, err := NewClient(ClientConfig{Token: tok, APIURL: "https://example.invalid"}); err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if got := redact("leaked " + tok + " here"); strings.Contains(got, tok) {
		t.Errorf("token was not registered for redaction: %q", got)
	}
}

func TestNewClientRejectsEmptyToken(t *testing.T) {
	_, err := NewClient(ClientConfig{})
	if err == nil {
		t.Fatal("NewClient accepted an empty token")
	}
	if !strings.Contains(err.Error(), "WIKI_OAUTH_TOKEN") {
		t.Errorf("error does not tell the user where to put the token: %v", err)
	}
}

func TestClientContextCancellation(t *testing.T) {
	f := newFakeWiki(t)
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, `{"id":"1","content":"x"}`, nil
	}
	c, _ := f.newTestClient(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.GetJSON(ctx, "/v1/pages", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", time.Now().Add(2*time.Second).UTC().Format(http.TimeFormat))

	if d := retryAfter(resp); d <= 0 || d > 3*time.Second {
		t.Errorf("retryAfter(date) = %v, want ~2s", d)
	}
}
