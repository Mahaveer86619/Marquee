// Package httpx is the HTTP client used for external services (metadata
// providers, release sources, images). It retries transient failures, keeps
// connections alive, surfaces provider error messages and keeps credentials
// out of error text.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrUnauthorized means the service rejected the credentials.
	ErrUnauthorized = errors.New("credentials rejected by the provider")
	// ErrNotFound means the service has no such resource.
	ErrNotFound = errors.New("not found")
	// ErrRateLimited means the service kept answering 429 after retries.
	ErrRateLimited = errors.New("rate limited by the provider")
)

// RetryBaseDelay is the base backoff between attempts. Tests may lower it.
var RetryBaseDelay = 150 * time.Millisecond

// Attempt limits per failure class.
const (
	// Connection or TLS failures fail in ~0.1 s on the networks we have seen
	// drop new connections (notes F-020), so many quick retries are cheap.
	connectionAttempts = 12
	serverAttempts     = 3 // 429 and 5xx responses
	maxConnectionWait  = 500 * time.Millisecond
)

// UserAgent is sent with every request, as the providers ask.
var UserAgent = "Marquee (https://github.com/Mahaveer86619/Marquee)"

// MaxBodyBytes caps downloaded bodies read with GetBytes.
const MaxBodyBytes = 8 << 20

// NewClient returns a client that keeps connections open for five minutes, so
// later requests reuse a working connection instead of opening a new one.
func NewClient() *http.Client {
	return NewClientWithTimeout(10 * time.Second)
}

// NewClientWithTimeout is NewClient with a custom per-request timeout, for
// services that are slow by nature (e.g. Prowlarr waiting on its indexers).
func NewClientWithTimeout(d time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.IdleConnTimeout = 5 * time.Minute
	transport.MaxIdleConnsPerHost = 4
	return &http.Client{Timeout: d, Transport: transport}
}

// GetJSON performs a GET and decodes a JSON response, with retries.
func GetJSON(ctx context.Context, client *http.Client, rawURL string, header http.Header, out any) error {
	return Fetch(ctx, client, rawURL, header, func(r io.Reader, _ *http.Response) error {
		if err := json.NewDecoder(r).Decode(out); err != nil {
			return fmt.Errorf("invalid response from %s: %w", Redact(rawURL), err)
		}
		return nil
	})
}

// GetBytes performs a GET and returns the body and content type, with retries.
func GetBytes(ctx context.Context, client *http.Client, rawURL string, header http.Header) ([]byte, string, error) {
	var body []byte
	var contentType string
	err := Fetch(ctx, client, rawURL, header, func(r io.Reader, resp *http.Response) error {
		b, err := io.ReadAll(io.LimitReader(r, MaxBodyBytes+1))
		if err != nil {
			return err
		}
		if len(b) > MaxBodyBytes {
			return fmt.Errorf("response from %s is larger than %d bytes", Redact(rawURL), MaxBodyBytes)
		}
		body, contentType = b, resp.Header.Get("Content-Type")
		return nil
	})
	return body, contentType, err
}

// Fetch performs a GET with retries for transient failures: connection and
// TLS errors, HTTP 429 (honouring Retry-After) and 5xx. Client errors (4xx)
// are returned immediately. decode reads a 200 response.
func Fetch(ctx context.Context, client *http.Client, rawURL string, header http.Header, decode func(io.Reader, *http.Response) error) error {
	var lastErr error
	for attempt := 1; ; attempt++ {
		wait, limit, err := once(ctx, client, rawURL, header, decode)
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

// once makes one attempt. For retryable errors it returns the base wait and
// the attempt limit of that failure class (0 = do not retry).
func once(ctx context.Context, client *http.Client, rawURL string, header http.Header, decode func(io.Reader, *http.Response) error) (time.Duration, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, 0, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		// A timeout means the service is slow, not that the connection was
		// dropped: retrying would repeat the whole slow request.
		var nerr net.Error
		timedOut := errors.As(err, &nerr) && nerr.Timeout()
		// url.Error embeds the full URL, which can carry an api_key parameter.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = fmt.Errorf("%s %s: %w", uerr.Op, Redact(uerr.URL), uerr.Err)
		}
		if timedOut {
			return 0, 0, err
		}
		return RetryBaseDelay, connectionAttempts, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return 0, 0, decode(resp.Body, resp)
	}

	msg := errorMessage(resp)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
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
// {"status_code":N,"status_message":"..."}; TVmaze and Prowlarr with
// {"message":"..."}.
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

// Redact removes the query string from a URL so credentials never reach logs.
func Redact(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i] + "?[redacted]"
	}
	return raw
}

// Reason is a short, user-facing description of a request failure.
func Reason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrUnauthorized):
		return "credentials rejected"
	case errors.Is(err, ErrRateLimited):
		return "rate limited"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case strings.Contains(err.Error(), "EOF"), strings.Contains(err.Error(), "connection"):
		return "connection failed"
	default:
		return "provider error"
	}
}
