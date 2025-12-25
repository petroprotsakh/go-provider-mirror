// Package version provides version information and User-Agent handling.
package version

import (
	"fmt"
	"net/http"
	"runtime"
)

// Set via ldflags at build time:
//
//	go build -ldflags "-X github.com/petroprotsakh/go-provider-mirror/internal/version.Version=v1.0.0"
var (
	// Version is the semantic version
	Version = "dev"
	// Commit is the git commit SHA
	Commit = "unknown"
	// BuildTime is the build timestamp
	BuildTime = "unknown"
)

// UserAgent returns the User-Agent string for HTTP requests.
func UserAgent() string {
	return fmt.Sprintf(
		"provider-mirror/%s (%s/%s; +https://github.com/petroprotsakh/go-provider-mirror)",
		Version,
		runtime.GOOS,
		runtime.GOARCH,
	)
}

// Transport wraps an http.RoundTripper to add User-Agent header.
type Transport struct {
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	if req2.Header.Get("User-Agent") == "" {
		req2.Header.Set("User-Agent", UserAgent())
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}
