// Package client is the internal HTTP client for the IDCloudHost API.
// It centralizes apikey header injection, the errors-in-a-200-body edge
// case, and retry-with-backoff on 429/5xx responses.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL    = "https://api.idcloudhost.com"
	defaultMaxRetries = 4
	defaultRetryBase  = 500 * time.Millisecond
)

// Client is a small, context-aware HTTP client for the IDCloudHost API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string

	maxRetries int
	retryBase  time.Duration
}

// Option customizes a Client at construction time.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client (tests use this to
// point at an httptest.Server).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithBaseURL overrides the API base URL (tests use this to point at an
// httptest.Server).
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithRetryPolicy overrides the retry count/backoff base for tests that
// need fast, deterministic retries.
func WithRetryPolicy(maxRetries int, retryBase time.Duration) Option {
	return func(c *Client) {
		c.maxRetries = maxRetries
		c.retryBase = retryBase
	}
}

// New creates a Client that sends apiKey as the `apikey` header on every
// request.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		httpClient: http.DefaultClient,
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		maxRetries: defaultMaxRetries,
		retryBase:  defaultRetryBase,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// apiError is the shape IDCloudHost uses for the `errors` key that can
// appear even on an HTTP 200 response.
type apiError struct {
	Errors json.RawMessage `json:"errors"`
}

// APIError is returned when the API reports a failure, whether via a
// non-2xx status code or an `errors` key in an otherwise-200 body.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("idcloudhost: api error (status %d): %s", e.StatusCode, e.Body)
}

// NotFound reports whether err represents a 404 from the API.
func NotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.StatusCode == http.StatusNotFound
}

// doForm sends a form-urlencoded request (the API's expected request
// encoding) and, on success, decodes the JSON response body into out
// (which may be nil if the caller doesn't need the payload).
func (c *Client) doForm(ctx context.Context, method, path string, form url.Values, out any) error {
	var encoded string
	if form != nil {
		encoded = form.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, backoffDuration(c.retryBase, attempt)); err != nil {
				return err
			}
		}

		var reqBody io.Reader
		if form != nil {
			reqBody = bytes.NewReader([]byte(encoded))
		}

		status, respBody, err := c.doOnce(ctx, method, path, reqBody)
		if err != nil {
			// Network-level error: retryable.
			lastErr = err
			continue
		}

		if status == http.StatusTooManyRequests || status >= http.StatusInternalServerError {
			lastErr = &APIError{StatusCode: status, Body: string(respBody)}
			continue
		}

		if status < 200 || status >= 300 {
			return &APIError{StatusCode: status, Body: string(respBody)}
		}

		// IDCloudHost can return HTTP 200 with an `errors` payload in the
		// body. Check for that before treating the call as a success.
		var maybeErr apiError
		if err := json.Unmarshal(respBody, &maybeErr); err == nil && len(maybeErr.Errors) > 0 && string(maybeErr.Errors) != "null" {
			return &APIError{StatusCode: status, Body: string(respBody)}
		}

		if out != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("idcloudhost: decoding response body: %w", err)
			}
		}
		return nil
	}

	return fmt.Errorf("idcloudhost: request failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

func (c *Client) doOnce(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("idcloudhost: building request: %w", err)
	}
	req.Header.Set("apikey", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("idcloudhost: sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("idcloudhost: reading response body: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

func backoffDuration(base time.Duration, attempt int) time.Duration {
	return time.Duration(float64(base) * math.Pow(2, float64(attempt-1)))
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
