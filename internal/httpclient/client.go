package httpclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/petroprotsakh/go-provider-mirror/internal/logging"
	"github.com/petroprotsakh/go-provider-mirror/internal/version"
)

// Config configures the HTTP client behavior.
type Config struct {
	Timeout    time.Duration
	Retries    int
	MaxBackoff time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Timeout:    30 * time.Second,
		Retries:    3,
		MaxBackoff: 60 * time.Second,
	}
}

// Client is a shared HTTP client with retry and auth support.
type Client struct {
	http        *http.Client
	credentials map[string]string // hostname -> token
	retries     int
	maxBackoff  time.Duration
	userAgent   string
	log         *logging.Logger
}

// New creates a new HTTP client.
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultConfig().Timeout
	}
	if cfg.Retries <= 0 {
		cfg.Retries = DefaultConfig().Retries
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = DefaultConfig().MaxBackoff
	}

	return &Client{
		http:        &http.Client{Timeout: cfg.Timeout},
		credentials: loadCredentials(),
		retries:     cfg.Retries,
		maxBackoff:  cfg.MaxBackoff,
		userAgent:   version.UserAgent(),
		log:         logging.Default(),
	}
}

// loadCredentials loads registry credentials from environment variables.
// Supports two prefixes (checked in order):
//   - PM_TOKEN_<hostname_with_underscores>=<token>
//   - TF_TOKEN_<hostname_with_underscores>=<token> (Terraform compatibility)
//
// Example: PM_TOKEN_registry_terraform_io=xxx
func loadCredentials() map[string]string {
	creds := make(map[string]string)

	prefixes := []string{"PM_TOKEN_", "TF_TOKEN_"}

	for _, env := range os.Environ() {
		for _, prefix := range prefixes {
			if !strings.HasPrefix(env, prefix) {
				continue
			}

			parts := strings.SplitN(env, "=", 2)
			if len(parts) != 2 {
				continue
			}

			// Convert PREFIX_registry_terraform_io -> registry.terraform.io
			hostname := strings.TrimPrefix(parts[0], prefix)
			hostname = strings.ReplaceAll(hostname, "__", "\x00") // preserve double underscores
			hostname = strings.ReplaceAll(hostname, "_", ".")
			hostname = strings.ReplaceAll(hostname, "\x00", "_") // restore as single underscore

			// Don't overwrite if already set (PM_TOKEN_ takes precedence)
			if _, exists := creds[hostname]; !exists {
				creds[hostname] = parts[1]
			}
		}
	}

	return creds
}

// RequestOption configures a request.
type RequestOption func(*requestOptions)

type requestOptions struct {
	hostname string // for auth
	retry    bool
}

// WithAuth adds authorization header for the given hostname.
func WithAuth(hostname string) RequestOption {
	return func(o *requestOptions) {
		o.hostname = hostname
	}
}

// WithRetry enables retry logic for transient failures (429, 5xx).
func WithRetry() RequestOption {
	return func(o *requestOptions) {
		o.retry = true
	}
}

// Do performs an HTTP request with optional auth and retry.
// Always adds User-Agent header.
func (c *Client) Do(req *http.Request, opts ...RequestOption) (*http.Response, error) {
	var o requestOptions
	for _, opt := range opts {
		opt(&o)
	}

	if o.retry {
		return c.doWithRetry(req.Context(), req, o.hostname)
	}

	c.addUserAgent(req)
	if o.hostname != "" {
		c.addAuth(req, o.hostname)
	}
	return c.http.Do(req)
}

func (c *Client) doWithRetry(
	ctx context.Context,
	req *http.Request,
	hostname string,
) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			backoff := Backoff(attempt, c.maxBackoff, lastErr)
			c.log.Debug(
				"retrying request",
				"attempt", attempt+1,
				"max_attempts", c.retries+1,
				"backoff", backoff,
				"url", req.URL.String(),
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		// Clone request for retry
		reqClone := req.Clone(ctx)
		c.addUserAgent(reqClone)
		if hostname != "" {
			c.addAuth(reqClone, hostname)
		}

		resp, err := c.http.Do(reqClone)
		if err != nil {
			lastErr = &RetryableError{Err: fmt.Errorf("request failed: %w", err)}
			continue
		}

		if isRetryableStatus(resp.StatusCode) {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			resp.Body.Close() //nolint:errcheck
			lastErr = &RetryableError{
				Err:        fmt.Errorf("HTTP %d", resp.StatusCode),
				RetryAfter: retryAfter,
			}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// addUserAgent adds the User-Agent header if not already set.
func (c *Client) addUserAgent(req *http.Request) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
}

// addAuth adds authorization header if credentials exist for the hostname.
func (c *Client) addAuth(req *http.Request, hostname string) {
	if token, ok := c.credentials[hostname]; ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// isRetryableStatus returns true for HTTP status codes that should be retried.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		return false
	}
}

// parseRetryAfter parses the Retry-After header value.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	return 0
}

// RetryableError indicates an error that can be retried.
type RetryableError struct {
	Err        error
	RetryAfter time.Duration
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// Backoff calculates backoff duration for a retry attempt.
// Uses Retry-After from lastErr if available, otherwise exponential backoff with jitter.
func Backoff(attempt int, maxBackoff time.Duration, lastErr error) time.Duration {
	var re *RetryableError
	if errors.As(lastErr, &re) && re.RetryAfter > 0 {
		if re.RetryAfter <= maxBackoff {
			return re.RetryAfter
		}
		return maxBackoff
	}

	baseBackoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	jitter := time.Duration(float64(baseBackoff) * (0.5 - rand.Float64()) * 0.5)
	backoff := baseBackoff + jitter

	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	return backoff
}

// NewHTTPError creates an error from an HTTP response.
// Returns *RetryableError for 429 and 5xx, plain error otherwise.
func NewHTTPError(resp *http.Response) error {
	statusCode := resp.StatusCode
	err := fmt.Errorf("HTTP %d", statusCode)

	if isRetryableStatus(statusCode) {
		return &RetryableError{
			Err:        err,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	return err
}
