package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultAPIURL is the wiki API host. It is used for REQUESTS ONLY — never as a
// base for user-facing page links (see Config.PageBaseURL in wiki.go).
const DefaultAPIURL = "https://api.wiki.yandex.net"

// maxErrorBody caps how much of an error response body is read, so a
// misconfigured endpoint returning an HTML page cannot flood memory or logs.
const maxErrorBody = 8 << 10

// ClientConfig configures the HTTP client.
type ClientConfig struct {
	APIURL string
	// Token is the user OAuth token. It comes from the environment or a
	// permission-checked file — NEVER from argv, where it would be visible in ps
	// output and shell history.
	Token string
	// OrgID identifies the organization; OrgHeader selects which header carries
	// it: "X-Org-Id" for Yandex 360 for Business, "X-Cloud-Org-Id" for Cloud Org.
	OrgID     string
	OrgHeader string

	Timeout    time.Duration
	MaxRetries int
	// MinInterval throttles requests: at least this long between two calls.
	MinInterval time.Duration
	// MaxRequests is a global circuit breaker on the whole crawl. Zero means
	// unlimited, which is only appropriate in tests.
	MaxRequests int

	// BaseBackoff and MaxBackoff shape the exponential retry delay. They are
	// injected rather than hardcoded so tests can run the full retry ladder in
	// microseconds instead of seconds.
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	// MaxRetryAfter bounds how long a server-supplied Retry-After is obeyed.
	// The hint itself is always honoured in full when it fits — clamping it down
	// would mean retrying sooner than the server asked and making the rate
	// limiting worse. A hint beyond this bound aborts the request instead of
	// parking the crawl for hours. Default: 2 minutes.
	MaxRetryAfter time.Duration
	// Sleep is the delay function; nil means time.Sleep. Tests substitute a
	// recorder to assert on the backoff schedule without waiting.
	Sleep func(time.Duration)
	// HTTPClient overrides the transport; nil builds one from Timeout.
	HTTPClient *http.Client
	// Rand seeds the backoff jitter; nil means a package-local source.
	Rand *rand.Rand
}

// ErrRequestBudget is returned once MaxRequests has been spent. It is a hard
// stop, not a truncation: the caller must see that the crawl was incomplete.
var ErrRequestBudget = errors.New("wiki: request budget exhausted")

// APIError is a non-2xx response from the wiki API.
type APIError struct {
	Status       int
	ErrorCode    string
	DebugMessage string
	Details      string
	// Retryable records the client's classification, so a caller can distinguish
	// "gave up after retries" from "never worth retrying".
	Retryable bool
}

// Error renders the API error. DebugMessage and Details are run through redact()
// because the API echoes parts of the request back — including, on a bad auth
// header, the token itself.
func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "wiki API: HTTP %d", e.Status)
	if e.ErrorCode != "" {
		fmt.Fprintf(&b, " (%s)", redact(e.ErrorCode))
	}
	if e.DebugMessage != "" {
		fmt.Fprintf(&b, ": %s", redact(e.DebugMessage))
	}
	if e.Details != "" {
		fmt.Fprintf(&b, " [%s]", redact(e.Details))
	}
	if e.Status == http.StatusUnauthorized {
		b.WriteString("\n  The wiki API authorizes USER accounts only — service accounts are not supported." +
			"\n  Obtain an OAuth token for a user account that can read the pages and put it in WIKI_OAUTH_TOKEN.")
	}
	if e.Status == http.StatusForbidden {
		b.WriteString("\n  The token is valid but this account may not read this page, or the org header is wrong" +
			" (X-Org-Id for Yandex 360 for Business, X-Cloud-Org-Id for Cloud Org).")
	}
	return b.String()
}

// Client is a rate-limited, retrying JSON client for the wiki API.
type Client struct {
	cfg  ClientConfig
	http *http.Client

	mu       sync.Mutex
	requests int
	lastCall time.Time
	rnd      *rand.Rand
}

// NewClient validates the config and builds a Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Token == "" {
		return nil, errors.New("wiki: OAuth token is empty — set WIKI_OAUTH_TOKEN or use --token-file (a user account token; service accounts are not supported)")
	}
	if cfg.APIURL == "" {
		cfg.APIURL = DefaultAPIURL
	}
	if cfg.OrgHeader == "" {
		cfg.OrgHeader = "X-Org-Id"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	if cfg.MaxRetryAfter <= 0 {
		cfg.MaxRetryAfter = 2 * time.Minute
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}

	// The token is now known: register it so redact() masks it everywhere,
	// including in error text produced by code that never saw the config.
	RegisterSecret(cfg.Token)

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	rnd := cfg.Rand
	if rnd == nil {
		rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	return &Client{cfg: cfg, http: httpClient, rnd: rnd}, nil
}

// Requests reports how many HTTP requests have been issued. Tests assert on it;
// the crawler reports it so a user can see the cost of a run.
func (c *Client) Requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

// URL renders the absolute request URL for path and q. GetJSON goes through it,
// so a diagnostic that prints this shows the request that is actually issued —
// there is no second place where the endpoint string is assembled.
//
// url.Values.Encode sorts by key and preserves the order of repeated values, so
// a repeated array parameter comes out as fields=content&fields=breadcrumbs.
func (c *Client) URL(path string, q url.Values) string {
	endpoint := strings.TrimRight(c.cfg.APIURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	return endpoint
}

// GetJSON performs GET {APIURL}{path}?{q} and returns the raw JSON body.
// Retries 429 and 5xx with exponential backoff plus jitter, honouring
// Retry-After when present. 400/401/403/404 are returned immediately: they are
// configuration or permission errors, and retrying them only burns the budget
// and, on 401, risks tripping an account lockout.
func (c *Client) GetJSON(ctx context.Context, path string, q url.Values) (json.RawMessage, error) {
	endpoint := c.URL(path, q)

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := c.reserve(ctx); err != nil {
			return nil, err
		}

		body, retryAfter, err := c.do(ctx, endpoint)
		if err == nil {
			return body, nil
		}

		var apiErr *APIError
		if !errors.As(err, &apiErr) || !apiErr.Retryable {
			return nil, err
		}
		if attempt >= c.cfg.MaxRetries {
			return nil, fmt.Errorf("wiki: giving up after %d retries: %w", c.cfg.MaxRetries, err)
		}

		delay, ok := c.backoff(attempt, retryAfter)
		if !ok {
			return nil, fmt.Errorf("wiki: server asked to retry after %v, which exceeds the %v limit — "+
				"the API is rate limiting this account hard; retry the crawl later: %w",
				retryAfter, c.cfg.MaxRetryAfter, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		c.cfg.Sleep(delay)
	}
}

// reserve enforces the global request budget and the minimum spacing between
// calls. Both counters are taken before the request is issued, so a budget of N
// can never result in N+1 requests.
func (c *Client) reserve(ctx context.Context) error {
	c.mu.Lock()
	if c.cfg.MaxRequests > 0 && c.requests >= c.cfg.MaxRequests {
		c.mu.Unlock()
		return fmt.Errorf("%w: %d requests (raise --max-requests if the crawl is legitimately this large)", ErrRequestBudget, c.cfg.MaxRequests)
	}
	c.requests++
	wait := time.Duration(0)
	if c.cfg.MinInterval > 0 && !c.lastCall.IsZero() {
		if elapsed := time.Since(c.lastCall); elapsed < c.cfg.MinInterval {
			wait = c.cfg.MinInterval - elapsed
		}
	}
	c.lastCall = time.Now().Add(wait)
	c.mu.Unlock()

	if wait > 0 {
		c.cfg.Sleep(wait)
	}
	return ctx.Err()
}

// do issues one request and classifies the outcome.
func (c *Client) do(ctx context.Context, endpoint string) (json.RawMessage, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("wiki: build request: %w", redactErr(err))
	}
	req.Header.Set("Authorization", "OAuth "+c.cfg.Token)
	req.Header.Set("Accept", "application/json")
	if c.cfg.OrgID != "" {
		req.Header.Set(c.cfg.OrgHeader, c.cfg.OrgID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// A transport error (DNS, connection reset, timeout) is worth retrying;
		// the URL is redacted because it may carry a token-bearing query param.
		return nil, 0, &APIError{Status: 0, DebugMessage: redact(err.Error()), Retryable: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, 0, &APIError{Status: resp.StatusCode, DebugMessage: redact(err.Error()), Retryable: true}
		}
		return body, 0, nil
	}

	apiErr := parseAPIError(resp)
	return nil, retryAfter(resp), apiErr
}

// parseAPIError builds an APIError from a non-200 response, decoding the
// documented {"debug_message","details","error_code"} envelope when present.
func parseAPIError(resp *http.Response) *APIError {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	e := &APIError{Status: resp.StatusCode, Retryable: isRetryable(resp.StatusCode)}

	var env struct {
		DebugMessage string          `json:"debug_message"`
		Details      json.RawMessage `json:"details"`
		ErrorCode    string          `json:"error_code"`
		Message      string          `json:"message"`
	}
	if json.Unmarshal(raw, &env) == nil {
		e.ErrorCode = env.ErrorCode
		e.DebugMessage = env.DebugMessage
		if e.DebugMessage == "" {
			e.DebugMessage = env.Message
		}
		if len(env.Details) > 0 && string(env.Details) != "null" {
			e.Details = string(env.Details)
		}
	}
	if e.DebugMessage == "" && len(raw) > 0 {
		e.DebugMessage = strings.TrimSpace(string(raw))
	}
	return e
}

// isRetryable classifies a status code. 429 and 5xx are transient. 400/401/403/
// 404 are not: no amount of retrying fixes a wrong token, a missing permission
// or a typo'd slug.
func isRetryable(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	return status >= 500 && status <= 599
}

// retryAfter reads the Retry-After header, in either of its two documented
// forms (delta-seconds or an HTTP-date).
func retryAfter(resp *http.Response) time.Duration {
	v := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// backoff computes the delay before retry number attempt (0-based). ok=false
// means the wait is unacceptably long and the request should fail instead.
//
// Retry-After takes priority over our own schedule and is obeyed IN FULL: the
// server knows its rate limits, and shortening its instruction only deepens the
// throttling. It is not clamped down to MaxBackoff — only refused outright when
// it exceeds MaxRetryAfter.
func (c *Client) backoff(attempt int, serverHint time.Duration) (time.Duration, bool) {
	if serverHint > 0 {
		if serverHint > c.cfg.MaxRetryAfter {
			return 0, false
		}
		return serverHint, true
	}

	delay := c.cfg.BaseBackoff << attempt
	if delay <= 0 || delay > c.cfg.MaxBackoff { // <= 0 guards the shift overflowing
		delay = c.cfg.MaxBackoff
	}

	// Full jitter over [delay/2, delay): spreads retries when several crawls hit
	// the same rate limit at once.
	half := delay / 2
	c.mu.Lock()
	j := time.Duration(c.rnd.Int63n(int64(half) + 1))
	c.mu.Unlock()
	return half + j, true
}

// redactErr wraps an error so its text is masked.
func redactErr(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(redact(err.Error()))
}
