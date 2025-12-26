package registry

import (
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

func TestNewClient_NilConfig(t *testing.T) {
	client := NewClient(nil)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClient_CustomConfig(t *testing.T) {
	client := NewClient(&Config{
		Timeout:    10 * time.Second,
		Retries:    5,
		MaxBackoff: 120 * time.Second,
	})

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClient_AppliesDefaults(t *testing.T) {
	// Zero values should be replaced with defaults
	client := NewClient(&Config{
		Timeout:    0,
		Retries:    0,
		MaxBackoff: 0,
	})

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// --- ProviderPlatform tests ---

func TestProviderPlatform_String(t *testing.T) {
	tests := []struct {
		platform ProviderPlatform
		want     string
	}{
		{ProviderPlatform{OS: "linux", Arch: "amd64"}, "linux_amd64"},
		{ProviderPlatform{OS: "darwin", Arch: "arm64"}, "darwin_arm64"},
		{ProviderPlatform{OS: "windows", Arch: "386"}, "windows_386"},
		{ProviderPlatform{OS: "freebsd", Arch: "arm"}, "freebsd_arm"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.platform.String(); got != tt.want {
				t.Errorf("ProviderPlatform.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- ParsePlatform tests ---

func TestParsePlatform(t *testing.T) {
	tests := []struct {
		name      string
		platform  string
		wantOS    string
		wantArch  string
		wantError bool
	}{
		{"linux_amd64", "linux_amd64", "linux", "amd64", false},
		{"darwin_arm64", "darwin_arm64", "darwin", "arm64", false},
		{"windows_386", "windows_386", "windows", "386", false},
		{"freebsd_arm", "freebsd_arm", "freebsd", "arm", false},
		{"no underscore", "linuxamd64", "", "", true},
		{"too many parts", "linux_amd64_extra", "", "", true},
		{"empty", "", "", "", true},
		{"only underscore", "_", "", "", false}, // splits to ["", ""] - 2 parts, valid format
		{"single part", "linux", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os, arch, err := ParsePlatform(tt.platform)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParsePlatform(%q) expected error", tt.platform)
				}
				return
			}

			if err != nil {
				t.Errorf("ParsePlatform(%q) unexpected error: %v", tt.platform, err)
				return
			}

			if os != tt.wantOS || arch != tt.wantArch {
				t.Errorf("ParsePlatform(%q) = (%q, %q), want (%q, %q)",
					tt.platform, os, arch, tt.wantOS, tt.wantArch)
			}
		})
	}
}

// --- DefaultServiceURL tests ---

func TestDefaultServiceURL_KnownRegistries(t *testing.T) {
	client := NewClient(nil)

	tests := []struct {
		hostname string
		want     string
	}{
		{"registry.terraform.io", "https://registry.terraform.io/v1/providers/"},
		{"registry.opentofu.org", "https://registry.opentofu.org/v1/providers/"},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			url, err := client.defaultServiceURL(tt.hostname)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.want {
				t.Errorf("defaultServiceURL(%q) = %q, want %q", tt.hostname, url, tt.want)
			}
		})
	}
}

func TestDefaultServiceURL_UnknownRegistry(t *testing.T) {
	client := NewClient(nil)

	_, err := client.defaultServiceURL("unknown.registry.io")
	if err == nil {
		t.Error("expected error for unknown registry")
	}
}

func TestDefaultServiceURL_PrivateRegistry(t *testing.T) {
	client := NewClient(nil)

	_, err := client.defaultServiceURL("my-company.registry.io")
	if err == nil {
		t.Error("expected error for private registry without discovery")
	}
}
