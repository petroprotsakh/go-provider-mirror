package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// --- Engine tests ---

func TestEngine_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		engine Engine
		want   bool
	}{
		{"terraform is valid", EngineTerraform, true},
		{"opentofu is valid", EngineOpenTofu, true},
		{"empty is invalid", Engine(""), false},
		{"unknown is invalid", Engine("pulumi"), false},
		{"case sensitive", Engine("Terraform"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.engine.IsValid(); got != tt.want {
				t.Errorf("Engine(%q).IsValid() = %v, want %v", tt.engine, got, tt.want)
			}
		})
	}
}

func TestEngine_DefaultRegistry(t *testing.T) {
	tests := []struct {
		name   string
		engine Engine
		want   string
	}{
		{"terraform registry", EngineTerraform, "registry.terraform.io"},
		{"opentofu registry", EngineOpenTofu, "registry.opentofu.org"},
		{"unknown returns empty", Engine("unknown"), ""},
		{"empty returns empty", Engine(""), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.engine.DefaultRegistry(); got != tt.want {
				t.Errorf("Engine(%q).DefaultRegistry() = %q, want %q", tt.engine, got, tt.want)
			}
		})
	}
}

// --- ProviderSource tests ---

func TestProviderSource_String(t *testing.T) {
	tests := []struct {
		name   string
		source ProviderSource
		want   string
	}{
		{
			name:   "full address",
			source: ProviderSource{Hostname: "registry.terraform.io", Namespace: "hashicorp", Name: "aws"},
			want:   "registry.terraform.io/hashicorp/aws",
		},
		{
			name:   "empty hostname",
			source: ProviderSource{Hostname: "", Namespace: "hashicorp", Name: "aws"},
			want:   "/hashicorp/aws",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.String(); got != tt.want {
				t.Errorf("ProviderSource.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseProviderSource(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		want      ProviderSource
		wantError bool
	}{
		{
			name:   "namespace/name format",
			source: "hashicorp/aws",
			want:   ProviderSource{Namespace: "hashicorp", Name: "aws"},
		},
		{
			name:   "hostname/namespace/name format",
			source: "registry.terraform.io/hashicorp/aws",
			want:   ProviderSource{Hostname: "registry.terraform.io", Namespace: "hashicorp", Name: "aws"},
		},
		{
			name:   "opentofu registry",
			source: "registry.opentofu.org/hashicorp/null",
			want:   ProviderSource{Hostname: "registry.opentofu.org", Namespace: "hashicorp", Name: "null"},
		},
		{
			name:      "single part is invalid",
			source:    "aws",
			wantError: true,
		},
		{
			name:      "four parts is invalid",
			source:    "a/b/c/d",
			wantError: true,
		},
		{
			name:      "empty is invalid",
			source:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProviderSource(tt.source)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParseProviderSource(%q) expected error, got nil", tt.source)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseProviderSource(%q) unexpected error: %v", tt.source, err)
				return
			}

			if got != tt.want {
				t.Errorf("ParseProviderSource(%q) = %+v, want %+v", tt.source, got, tt.want)
			}
		})
	}
}

// --- Parse tests ---

func TestParse_ValidManifest(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
`
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(m.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(m.Providers))
	}

	if m.Providers[0].Source != "hashicorp/aws" {
		t.Errorf("expected source hashicorp/aws, got %s", m.Providers[0].Source)
	}

	// Check defaults were applied
	if len(m.Providers[0].Engines) != 1 || m.Providers[0].Engines[0] != EngineTerraform {
		t.Errorf("expected terraform engine to be applied from defaults")
	}

	if len(m.Providers[0].Platforms) != 1 || m.Providers[0].Platforms[0] != "linux_amd64" {
		t.Errorf("expected linux_amd64 platform to be applied from defaults")
	}
}

func TestParse_MultipleEngines(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
    - opentofu
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/null
    versions: ["3.2.4"]
`
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(m.Defaults.Engines) != 2 {
		t.Errorf("expected 2 engines in defaults, got %d", len(m.Defaults.Engines))
	}
}

func TestParse_ProviderOverridesDefaults(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
    engines:
      - opentofu
    platforms:
      - darwin_arm64
`
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	p := m.Providers[0]

	// Provider-level engines should override defaults
	if len(p.Engines) != 1 || p.Engines[0] != EngineOpenTofu {
		t.Errorf("expected opentofu engine override, got %v", p.Engines)
	}

	// Provider-level platforms should override defaults
	if len(p.Platforms) != 1 || p.Platforms[0] != "darwin_arm64" {
		t.Errorf("expected darwin_arm64 platform override, got %v", p.Platforms)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	yaml := `
this is not valid yaml: [
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

// --- Validation tests ---

func TestValidate_NoProviders(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
providers: []
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for empty providers")
	}
}

func TestValidate_UnsupportedEngine(t *testing.T) {
	yaml := `
defaults:
  engines:
    - pulumi
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for unsupported engine")
	}
}

func TestValidate_UnsupportedEngineOnProvider(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
    engines:
      - invalid
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for unsupported engine on provider")
	}
}

func TestValidate_MissingSource(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform

providers:
  - versions: ["~> 5.0"]
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestValidate_MissingVersions(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform

providers:
  - source: hashicorp/aws
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for missing versions")
	}
}

func TestValidate_NoEnginesAnywhere(t *testing.T) {
	yaml := `
providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error when no engines specified anywhere")
	}
}

func TestValidate_EngineOnProviderOnly(t *testing.T) {
	// Should be valid - engines on provider without defaults
	yaml := `
providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
    engines:
      - terraform
`
	_, err := Parse([]byte(yaml))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- GetExpandedProviders tests ---

func TestGetExpandedProviders_SingleEngine(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/null
    versions: ["3.2.4"]
`
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	expanded, err := m.GetExpandedProviders()
	if err != nil {
		t.Fatalf("GetExpandedProviders() error = %v", err)
	}

	if len(expanded) != 1 {
		t.Fatalf("expected 1 expanded provider, got %d", len(expanded))
	}

	ep := expanded[0]
	if ep.Source.Hostname != "registry.terraform.io" {
		t.Errorf("expected hostname registry.terraform.io, got %s", ep.Source.Hostname)
	}
	if ep.Source.Namespace != "hashicorp" {
		t.Errorf("expected namespace hashicorp, got %s", ep.Source.Namespace)
	}
	if ep.Source.Name != "null" {
		t.Errorf("expected name null, got %s", ep.Source.Name)
	}
	if ep.Engine != EngineTerraform {
		t.Errorf("expected engine terraform, got %s", ep.Engine)
	}
	if ep.SourceSpec != "hashicorp/null" {
		t.Errorf("expected SourceSpec hashicorp/null, got %s", ep.SourceSpec)
	}
}

func TestGetExpandedProviders_MultipleEngines(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
    - opentofu
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/null
    versions: ["3.2.4"]
`
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	expanded, err := m.GetExpandedProviders()
	if err != nil {
		t.Fatalf("GetExpandedProviders() error = %v", err)
	}

	if len(expanded) != 2 {
		t.Fatalf("expected 2 expanded providers, got %d", len(expanded))
	}

	// Check we have both registries
	hostnames := make(map[string]bool)
	for _, ep := range expanded {
		hostnames[ep.Source.Hostname] = true
	}

	if !hostnames["registry.terraform.io"] {
		t.Error("expected registry.terraform.io in expanded providers")
	}
	if !hostnames["registry.opentofu.org"] {
		t.Error("expected registry.opentofu.org in expanded providers")
	}
}

func TestGetExpandedProviders_ExplicitHostname(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
    - opentofu
  platforms:
    - linux_amd64

providers:
  - source: registry.opentofu.org/hashicorp/null
    versions: ["3.2.4"]
`
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	expanded, err := m.GetExpandedProviders()
	if err != nil {
		t.Fatalf("GetExpandedProviders() error = %v", err)
	}

	// Explicit hostname should NOT expand per engine
	if len(expanded) != 1 {
		t.Fatalf("expected 1 expanded provider for explicit hostname, got %d", len(expanded))
	}

	ep := expanded[0]
	if ep.Source.Hostname != "registry.opentofu.org" {
		t.Errorf("expected hostname registry.opentofu.org, got %s", ep.Source.Hostname)
	}
	if ep.Engine != "" {
		t.Errorf("expected empty engine for explicit hostname, got %s", ep.Engine)
	}
	if ep.SourceSpec != "registry.opentofu.org/hashicorp/null" {
		t.Errorf("expected SourceSpec to match original, got %s", ep.SourceSpec)
	}
}

func TestGetExpandedProviders_MultipleProviders(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
  - source: hashicorp/null
    versions: ["3.2.4"]
`
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	expanded, err := m.GetExpandedProviders()
	if err != nil {
		t.Fatalf("GetExpandedProviders() error = %v", err)
	}

	if len(expanded) != 2 {
		t.Fatalf("expected 2 expanded providers, got %d", len(expanded))
	}

	names := make(map[string]bool)
	for _, ep := range expanded {
		names[ep.Source.Name] = true
	}

	if !names["aws"] {
		t.Error("expected aws in expanded providers")
	}
	if !names["null"] {
		t.Error("expected null in expanded providers")
	}
}

func TestGetExpandedProviders_PreservesVersionsAndPlatforms(t *testing.T) {
	yaml := `
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64
    - darwin_arm64

providers:
  - source: hashicorp/aws
    versions: ["~> 5.0", "~> 4.0"]
`
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	expanded, err := m.GetExpandedProviders()
	if err != nil {
		t.Fatalf("GetExpandedProviders() error = %v", err)
	}

	ep := expanded[0]

	if len(ep.Versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(ep.Versions))
	}

	if len(ep.Platforms) != 2 {
		t.Errorf("expected 2 platforms, got %d", len(ep.Platforms))
	}
}

// --- Load tests ---

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/manifest.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	// Create a temporary file
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")

	content := `
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/null
    versions: ["3.2.4"]
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(m.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(m.Providers))
	}
}
