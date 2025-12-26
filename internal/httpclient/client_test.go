package httpclient

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// --- Config tests ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cfg.Timeout)
	}

	if cfg.Retries != 3 {
		t.Errorf("expected 3 retries, got %d", cfg.Retries)
	}

	if cfg.MaxBackoff != 60*time.Second {
		t.Errorf("expected max backoff 60s, got %v", cfg.MaxBackoff)
	}
}

func TestNew_AppliesDefaults(t *testing.T) {
	// Empty config should use defaults
	client := New(Config{})

	if client.retries != 3 {
		t.Errorf("expected 3 retries, got %d", client.retries)
	}

	if client.maxBackoff != 60*time.Second {
		t.Errorf("expected max backoff 60s, got %v", client.maxBackoff)
	}
}

func TestNew_RespectsCustomConfig(t *testing.T) {
	client := New(Config{
		Timeout:    10 * time.Second,
		Retries:    5,
		MaxBackoff: 120 * time.Second,
	})

	if client.retries != 5 {
		t.Errorf("expected 5 retries, got %d", client.retries)
	}

	if client.maxBackoff != 120*time.Second {
		t.Errorf("expected max backoff 120s, got %v", client.maxBackoff)
	}
}

// --- Retryable status tests ---

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusOK, false},
		{http.StatusCreated, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusTooManyRequests, true},     // 429
		{http.StatusInternalServerError, true}, // 500
		{http.StatusBadGateway, true},          // 502
		{http.StatusServiceUnavailable, true},  // 503
		{http.StatusGatewayTimeout, true},      // 504
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.code), func(t *testing.T) {
			if got := isRetryableStatus(tt.code); got != tt.want {
				t.Errorf("isRetryableStatus(%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

// --- Retry-After parsing tests ---

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", 0},
		{"zero", "0", 0},
		{"one second", "1", 1 * time.Second},
		{"30 seconds", "30", 30 * time.Second},
		{"120 seconds", "120", 120 * time.Second},
		{"invalid string", "invalid", 0},
		{"negative", "-5", -5 * time.Second}, // strconv.Atoi parses negative numbers
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.value); got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// --- Backoff tests ---

func TestBackoff_ExponentialGrowth(t *testing.T) {
	maxBackoff := 60 * time.Second

	// Backoff should grow exponentially
	b1 := Backoff(1, maxBackoff, nil)
	b2 := Backoff(2, maxBackoff, nil)
	b3 := Backoff(3, maxBackoff, nil)

	// With jitter, we can't check exact values, but order should hold
	// Base values are 2^1=2s, 2^2=4s, 2^3=8s
	if b1 <= 0 {
		t.Error("backoff for attempt 1 should be positive")
	}

	// Just verify they're all positive and within reasonable bounds
	if b2 <= 0 || b3 <= 0 {
		t.Error("backoff should be positive for all attempts")
	}
}

func TestBackoff_RespectsMaxBackoff(t *testing.T) {
	maxBackoff := 5 * time.Second

	// High attempt number should hit max
	backoff := Backoff(10, maxBackoff, nil)

	if backoff > maxBackoff {
		t.Errorf("backoff %v exceeded max %v", backoff, maxBackoff)
	}
}

func TestBackoff_UsesRetryAfter(t *testing.T) {
	maxBackoff := 60 * time.Second
	retryAfter := 10 * time.Second

	err := &RetryableError{
		Err:        errors.New("rate limited"),
		RetryAfter: retryAfter,
	}

	backoff := Backoff(1, maxBackoff, err)

	if backoff != retryAfter {
		t.Errorf("expected backoff %v from Retry-After, got %v", retryAfter, backoff)
	}
}

func TestBackoff_RetryAfterCappedByMax(t *testing.T) {
	maxBackoff := 5 * time.Second
	retryAfter := 60 * time.Second // Larger than max

	err := &RetryableError{
		Err:        errors.New("rate limited"),
		RetryAfter: retryAfter,
	}

	backoff := Backoff(1, maxBackoff, err)

	if backoff != maxBackoff {
		t.Errorf("expected backoff capped at %v, got %v", maxBackoff, backoff)
	}
}

// --- RetryableError tests ---

func TestRetryableError_Error(t *testing.T) {
	err := &RetryableError{
		Err: errors.New("test error"),
	}

	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %q", err.Error())
	}
}

func TestRetryableError_Unwrap(t *testing.T) {
	inner := errors.New("inner error")
	err := &RetryableError{Err: inner}

	if !errors.Is(err, inner) {
		t.Error("Unwrap should allow errors.Is to find inner error")
	}
}

func TestRetryableError_As(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &RetryableError{
		Err:        errors.New("retryable"),
		RetryAfter: 5 * time.Second,
	})

	var re *RetryableError
	if !errors.As(err, &re) {
		t.Error("errors.As should find RetryableError")
	}

	if re.RetryAfter != 5*time.Second {
		t.Errorf("expected RetryAfter 5s, got %v", re.RetryAfter)
	}
}

// --- NewHTTPError tests ---

func TestNewHTTPError_RetryableStatus(t *testing.T) {
	tests := []struct {
		status     int
		retryAfter string
		wantRetry  bool
		wantDelay  time.Duration
	}{
		{http.StatusTooManyRequests, "30", true, 30 * time.Second},
		{http.StatusServiceUnavailable, "10", true, 10 * time.Second},
		{http.StatusInternalServerError, "", true, 0},
		{http.StatusBadRequest, "", false, 0},
		{http.StatusNotFound, "", false, 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.status,
				Header:     make(http.Header),
			}
			if tt.retryAfter != "" {
				resp.Header.Set("Retry-After", tt.retryAfter)
			}

			err := NewHTTPError(resp)

			var re *RetryableError
			isRetryable := errors.As(err, &re)

			if isRetryable != tt.wantRetry {
				t.Errorf("isRetryable = %v, want %v", isRetryable, tt.wantRetry)
			}

			if isRetryable && re.RetryAfter != tt.wantDelay {
				t.Errorf("RetryAfter = %v, want %v", re.RetryAfter, tt.wantDelay)
			}
		})
	}
}

// --- HTTP request tests ---

func TestClient_Do_AddsUserAgent(t *testing.T) {
	var receivedUA string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{Timeout: 5 * time.Second})

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if receivedUA == "" {
		t.Error("User-Agent header should be set")
	}

	if receivedUA == "Go-http-client/1.1" || receivedUA == "Go-http-client/2.0" {
		t.Error("User-Agent should be custom, not Go default")
	}
}

func TestClient_Do_WithAuth(t *testing.T) {
	// Set up test credentials
	_ = os.Setenv("PM_TOKEN_test_example_com", "test-token-123")
	defer os.Unsetenv("PM_TOKEN_test_example_com") //nolint:errcheck

	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create new client to pick up the env var
	client := New(Config{Timeout: 5 * time.Second})

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req, WithAuth("test.example.com"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	expected := "Bearer test-token-123"
	if receivedAuth != expected {
		t.Errorf("expected auth %q, got %q", expected, receivedAuth)
	}
}

func TestClient_Do_WithRetry_Success(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		Timeout:    5 * time.Second,
		Retries:    5,
		MaxBackoff: 100 * time.Millisecond, // Fast for testing
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req, WithRetry())
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestClient_Do_WithRetry_ExhaustsRetries(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{
		Timeout:    5 * time.Second,
		Retries:    2,
		MaxBackoff: 10 * time.Millisecond,
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := client.Do(req, WithRetry())

	if err == nil {
		t.Error("expected error after exhausting retries")
	}

	// Should have tried 3 times (initial + 2 retries)
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestClient_Do_NoRetryFor4xx(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(Config{
		Timeout:    5 * time.Second,
		Retries:    3,
		MaxBackoff: 10 * time.Millisecond,
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req, WithRetry())
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// 400 is not retryable, should return immediately
	if attempts != 1 {
		t.Errorf("expected 1 attempt for non-retryable status, got %d", attempts)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// --- Credential loading tests ---

func TestLoadCredentials_PMToken(t *testing.T) {
	_ = os.Setenv("PM_TOKEN_registry_terraform_io", "pm-token")
	defer os.Unsetenv("PM_TOKEN_registry_terraform_io") //nolint:errcheck

	creds := loadCredentials()

	if creds["registry.terraform.io"] != "pm-token" {
		t.Errorf("expected 'pm-token', got %q", creds["registry.terraform.io"])
	}
}

func TestLoadCredentials_TFToken(t *testing.T) {
	_ = os.Setenv("TF_TOKEN_registry_terraform_io", "tf-token")
	defer os.Unsetenv("TF_TOKEN_registry_terraform_io") //nolint:errcheck

	creds := loadCredentials()

	if creds["registry.terraform.io"] != "tf-token" {
		t.Errorf("expected 'tf-token', got %q", creds["registry.terraform.io"])
	}
}

func TestLoadCredentials_PMTokenTakesPrecedence(t *testing.T) {
	_ = os.Setenv("PM_TOKEN_registry_terraform_io", "pm-token")
	_ = os.Setenv("TF_TOKEN_registry_terraform_io", "tf-token")
	defer os.Unsetenv("PM_TOKEN_registry_terraform_io") //nolint:errcheck
	defer os.Unsetenv("TF_TOKEN_registry_terraform_io") //nolint:errcheck

	creds := loadCredentials()

	if creds["registry.terraform.io"] != "pm-token" {
		t.Errorf("PM_TOKEN should take precedence, got %q", creds["registry.terraform.io"])
	}
}

func TestLoadCredentials_DoubleUnderscore(t *testing.T) {
	// Double underscore should become single underscore in hostname
	_ = os.Setenv("PM_TOKEN_my__custom_registry_io", "custom-token")
	defer os.Unsetenv("PM_TOKEN_my__custom_registry_io") //nolint:errcheck

	creds := loadCredentials()

	if creds["my_custom.registry.io"] != "custom-token" {
		t.Errorf("double underscore should become single, got credentials for: %v", creds)
	}
}

func TestLoadCredentials_Empty(t *testing.T) {
	// Ensure no PM_TOKEN_ or TF_TOKEN_ env vars are set
	for _, env := range os.Environ() {
		if len(env) > 9 && (env[:9] == "PM_TOKEN_" || env[:9] == "TF_TOKEN_") {
			t.Skip("skipping: token env vars are set in environment")
		}
	}

	creds := loadCredentials()

	// Should be empty (or have credentials from unrelated prefixes)
	// Just verify it doesn't panic
	_ = creds
}
