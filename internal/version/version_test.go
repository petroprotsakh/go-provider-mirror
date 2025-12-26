package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestUserAgent(t *testing.T) {
	ua := UserAgent()

	// Check it contains expected components
	if !strings.Contains(ua, "provider-mirror/") {
		t.Error("UserAgent should contain 'provider-mirror/'")
	}

	if !strings.Contains(ua, Version) {
		t.Errorf("UserAgent should contain version %q", Version)
	}

	if !strings.Contains(ua, runtime.GOOS) {
		t.Errorf("UserAgent should contain OS %q", runtime.GOOS)
	}

	if !strings.Contains(ua, runtime.GOARCH) {
		t.Errorf("UserAgent should contain arch %q", runtime.GOARCH)
	}

	if !strings.Contains(ua, "github.com/petroprotsakh/go-provider-mirror") {
		t.Error("UserAgent should contain project URL")
	}
}

func TestUserAgent_Format(t *testing.T) {
	ua := UserAgent()

	// Should match format: provider-mirror/VERSION (OS/ARCH; +URL)
	if !strings.HasPrefix(ua, "provider-mirror/") {
		t.Error("UserAgent should start with 'provider-mirror/'")
	}

	if !strings.Contains(ua, "(") || !strings.Contains(ua, ")") {
		t.Error("UserAgent should contain parentheses for system info")
	}

	if !strings.Contains(ua, "+https://") {
		t.Error("UserAgent should contain URL with + prefix")
	}
}

func TestDefaultValues(t *testing.T) {
	// Default values should be set (can be overridden by ldflags at build time)
	if Version == "" {
		t.Error("Version should not be empty")
	}

	if Commit == "" {
		t.Error("Commit should not be empty")
	}

	if BuildTime == "" {
		t.Error("BuildTime should not be empty")
	}
}
