// Package metadata searches and fetches film and series metadata from TMDB
// and TVmaze, and caches it in the core database.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"marquee/internal/catalog"
)

// Provider is a metadata source.
type Provider interface {
	Name() string
	Search(ctx context.Context, query string) ([]catalog.SearchResult, error)
	Title(ctx context.Context, ref catalog.Ref) (catalog.Title, error)
	Episodes(ctx context.Context, ref catalog.Ref, season int) ([]catalog.Episode, error)
}

var (
	// ErrUnauthorized means the provider rejected the credentials.
	ErrUnauthorized = errors.New("credentials rejected by the provider")
	// ErrNotFound means the provider has no such title.
	ErrNotFound = errors.New("not found")
	// ErrRateLimited means the provider kept answering 429 after retries.
	ErrRateLimited = errors.New("rate limited by the provider")
)

// RetryBaseDelay is the base backoff between attempts. Tests may lower it.
var RetryBaseDelay = 150 * time.Millisecond

// Attempt limits per failure class.
const (
	// Connection or TLS failures fail in ~0.1 s on the networks we have seen
	// drop new connections (F-020), so many quick retries are cheap.
	connectionAttempts = 12
	serverAttempts     = 3 // 429 and 5xx responses
	maxConnectionWait  = 500 * time.Millisecond
)

// userAgent is sent with every request, as the providers ask.
var userAgent = "Marquee (https://github.com/Mahaveer86619/Marquee)"

// getJSON performs a GET and decodes a JSON response, with retries (see fetch).
func getJSON(ctx context.Context, client *http.Client, rawURL string, header http.Header, out any) error {
	return fetch(ctx, client, rawURL, header, func(r io.Reader) error {
		if err := json.NewDecoder(r).Decode(out); err != nil {
			return fmt.Errorf("invalid response from %s: %w", redact(rawURL), err)
		}
		return nil
	})
}

// maxImageBytes caps downloaded images.
const maxImageBytes = 5 << 20

// getBytes performs a GET and returns the body and content type, with retries.
func getBytes(ctx context.Context, client *http.Client, rawURL string) ([]byte, string, error) {
	var body []byte
	var contentType string
	err := fetch(ctx, client, rawURL, nil, func(r io.Reader) error {
		b, err := io.ReadAll(io.LimitReader(r, maxImageBytes+1))
		if err != nil {
			return err
		}
		if len(b) > maxImageBytes {
			return fmt.Errorf("image from %s is larger than %d bytes", redact(rawURL), maxImageBytes)
		}
		body = b
		return nil
	}, func(resp *http.Response) { contentType = resp.Header.Get("Content-Type") })
	return body, contentType, err
}

// fetch performs a GET with retries for transient failures: connection and
// TLS errors, HTTP 429 (honouring Retry-After) and 5xx. Client errors (4xx)
// are returned immediately. decode reads a 200 response body; the optional
// inspect sees the response headers first.
func fetch(ctx context.Context, client *http.Client, rawURL string, header http.Header, decode func(io.Reader) error, inspect ...func(*http.Response)) error {
	var lastErr error
	for attempt := 1; ; attempt++ {
		wait, limit, err := doGet(ctx, client, rawURL, header, decode, inspect...)
		if err == nil {
			return nil
		}
		lastErr = err
		if limit == 0 || attempt >= limit || ctx.Err() != nil {
			return lastErr
		}
		delay := wait * time.Duration(attempt)
		if limit == connectionAttempts {
			delay = min(delay, maxConnectionWait)
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return lastErr
		}
	}
}

// doGet makes one attempt. It returns the error, and for retryable errors the
// base wait and the attempt limit for that failure class (0 = do not retry).
func doGet(ctx context.Context, client *http.Client, rawURL string, header http.Header, decode func(io.Reader) error, inspect ...func(*http.Response)) (time.Duration, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, 0, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		// url.Error embeds the full URL, which can carry an api_key parameter.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = fmt.Errorf("%s %s: %w", uerr.Op, redact(uerr.URL), uerr.Err)
		}
		return RetryBaseDelay, connectionAttempts, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		for _, f := range inspect {
			f(resp)
		}
		return 0, 0, decode(resp.Body)
	}

	msg := errorMessage(resp)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return 0, 0, fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case resp.StatusCode == http.StatusNotFound:
		return 0, 0, fmt.Errorf("%w: %s", ErrNotFound, msg)
	case resp.StatusCode == http.StatusTooManyRequests:
		wait := time.Second
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s >= 0 {
			wait = time.Duration(s) * time.Second
		}
		return wait, serverAttempts, fmt.Errorf("%w: %s", ErrRateLimited, msg)
	case resp.StatusCode >= 500:
		return RetryBaseDelay * 3, serverAttempts, fmt.Errorf("provider error %d: %s", resp.StatusCode, msg)
	default:
		return 0, 0, fmt.Errorf("request rejected (%d): %s", resp.StatusCode, msg)
	}
}

// errorMessage extracts the provider's error text. TMDB replies with
// {"status_code":N,"status_message":"..."}; TVmaze with {"message":"..."}.
func errorMessage(resp *http.Response) string {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	var body struct {
		StatusCode    int    `json:"status_code"`
		StatusMessage string `json:"status_message"`
		Message       string `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		switch {
		case body.StatusMessage != "":
			return fmt.Sprintf("%s (code %d)", body.StatusMessage, body.StatusCode)
		case body.Message != "":
			return body.Message
		}
	}
	if s := strings.TrimSpace(string(raw)); s != "" && len(s) < 200 {
		return s
	}
	return http.StatusText(resp.StatusCode)
}

// newHTTPClient keeps connections open for five minutes so later requests
// reuse a working connection instead of opening (and handshaking) a new one.
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.IdleConnTimeout = 5 * time.Minute
	transport.MaxIdleConnsPerHost = 4
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}
}

// redact removes the query string from a URL so credentials never reach logs.
func redact(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i] + "?[redacted]"
	}
	return raw
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

// plainText strips HTML tags and entities (TVmaze summaries are HTML).
func plainText(s string) string {
	return strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(s, "")))
}
