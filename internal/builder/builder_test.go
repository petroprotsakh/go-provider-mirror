package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- Config tests ---

func TestConfig_Fields(t *testing.T) {
	cfg := Config{
		ManifestPath: "/path/to/manifest.yaml",
		OutputDir:    "/path/to/output",
		CacheDir:     "/path/to/cache",
		NoCache:      true,
		Concurrency:  8,
		Retries:      3,
		MaxBackoff:   60,
	}

	if cfg.ManifestPath != "/path/to/manifest.yaml" {
		t.Errorf("unexpected ManifestPath: %s", cfg.ManifestPath)
	}

	if cfg.OutputDir != "/path/to/output" {
		t.Errorf("unexpected OutputDir: %s", cfg.OutputDir)
	}

	if cfg.CacheDir != "/path/to/cache" {
		t.Errorf("unexpected CacheDir: %s", cfg.CacheDir)
	}

	if !cfg.NoCache {
		t.Error("expected NoCache to be true")
	}

	if cfg.Concurrency != 8 {
		t.Errorf("expected Concurrency 8, got %d", cfg.Concurrency)
	}

	if cfg.Retries != 3 {
		t.Errorf("expected Retries 3, got %d", cfg.Retries)
	}

	if cfg.MaxBackoff != 60 {
		t.Errorf("expected MaxBackoff 60, got %d", cfg.MaxBackoff)
	}
}

// --- New tests ---

func TestNew_ValidManifest(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.yaml")

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
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	b, err := New(
		Config{
			ManifestPath: manifestPath,
			OutputDir:    filepath.Join(tmpDir, "output"),
			CacheDir:     filepath.Join(tmpDir, "cache"),
			Concurrency:  4,
			Retries:      3,
			MaxBackoff:   60,
		},
	)

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if b == nil {
		t.Fatal("expected non-nil builder")
	}

	if b.manifest == nil {
		t.Error("expected manifest to be loaded")
	}

	if b.client == nil {
		t.Error("expected client to be created")
	}

	if len(b.manifest.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(b.manifest.Providers))
	}
}

func TestNew_ManifestNotFound(t *testing.T) {
	_, err := New(
		Config{
			ManifestPath: "/nonexistent/manifest.yaml",
		},
	)

	if err == nil {
		t.Error("expected error for nonexistent manifest")
	}
}

func TestNew_InvalidManifest(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "invalid.yaml")

	// Write invalid YAML
	if err := os.WriteFile(manifestPath, []byte("not: [valid: yaml"), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	_, err := New(
		Config{
			ManifestPath: manifestPath,
		},
	)

	if err == nil {
		t.Error("expected error for invalid manifest")
	}
}

func TestNew_EmptyProviders(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "empty.yaml")

	content := `
defaults:
  engines:
    - terraform
providers: []
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	_, err := New(
		Config{
			ManifestPath: manifestPath,
		},
	)

	if err == nil {
		t.Error("expected error for empty providers")
	}
}

func TestNew_MissingEngines(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "no-engines.yaml")

	content := `
providers:
  - source: hashicorp/null
    versions: ["3.2.4"]
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	_, err := New(
		Config{
			ManifestPath: manifestPath,
		},
	)

	if err == nil {
		t.Error("expected error for missing engines")
	}
}

func TestNew_MultipleProviders(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "multi.yaml")

	content := `
defaults:
  engines:
    - terraform
    - opentofu
  platforms:
    - linux_amd64
    - darwin_arm64

providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
  - source: hashicorp/null
    versions: ["3.2.4"]
  - source: hashicorp/random
    versions: [">= 3.0"]
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	b, err := New(
		Config{
			ManifestPath: manifestPath,
			OutputDir:    filepath.Join(tmpDir, "output"),
		},
	)

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if len(b.manifest.Providers) != 3 {
		t.Errorf("expected 3 providers, got %d", len(b.manifest.Providers))
	}

	if len(b.manifest.Defaults.Engines) != 2 {
		t.Errorf("expected 2 engines, got %d", len(b.manifest.Defaults.Engines))
	}

	if len(b.manifest.Defaults.Platforms) != 2 {
		t.Errorf("expected 2 platforms, got %d", len(b.manifest.Defaults.Platforms))
	}
}

func TestNew_ProviderOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "override.yaml")

	content := `
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64

providers:
  - source: hashicorp/null
    versions: ["3.2.4"]
    engines:
      - opentofu
    platforms:
      - darwin_arm64
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	b, err := New(
		Config{
			ManifestPath: manifestPath,
		},
	)

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	p := b.manifest.Providers[0]

	// Provider-level overrides should be applied
	if len(p.Engines) != 1 || p.Engines[0] != "opentofu" {
		t.Errorf("expected opentofu engine override, got %v", p.Engines)
	}

	if len(p.Platforms) != 1 || p.Platforms[0] != "darwin_arm64" {
		t.Errorf("expected darwin_arm64 platform override, got %v", p.Platforms)
	}
}

func TestNew_ConfigPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.yaml")

	content := `
defaults:
  engines:
    - terraform
providers:
  - source: hashicorp/null
    versions: ["3.2.4"]
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	cfg := Config{
		ManifestPath: manifestPath,
		OutputDir:    "/custom/output",
		CacheDir:     "/custom/cache",
		NoCache:      true,
		Concurrency:  16,
		Retries:      5,
		MaxBackoff:   120,
	}

	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if b.config.OutputDir != "/custom/output" {
		t.Errorf("OutputDir not preserved: %s", b.config.OutputDir)
	}

	if b.config.CacheDir != "/custom/cache" {
		t.Errorf("CacheDir not preserved: %s", b.config.CacheDir)
	}

	if !b.config.NoCache {
		t.Error("NoCache not preserved")
	}

	if b.config.Concurrency != 16 {
		t.Errorf("Concurrency not preserved: %d", b.config.Concurrency)
	}

	if b.config.Retries != 5 {
		t.Errorf("Retries not preserved: %d", b.config.Retries)
	}

	if b.config.MaxBackoff != 120 {
		t.Errorf("MaxBackoff not preserved: %d", b.config.MaxBackoff)
	}
}

// --- Builder struct tests ---

func TestBuilder_HasRequiredFields(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.yaml")

	content := `
defaults:
  engines:
    - terraform
providers:
  - source: hashicorp/null
    versions: ["3.2.4"]
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	b, err := New(Config{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Check all required fields are initialized
	if b.config.ManifestPath == "" {
		t.Error("config.ManifestPath should be set")
	}

	if b.manifest == nil {
		t.Error("manifest should be loaded")
	}

	if b.client == nil {
		t.Error("client should be initialized")
	}

	if b.log == nil {
		t.Error("log should be initialized")
	}
}

// --- Context cancellation tests ---

func TestBuild_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.yaml")

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
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	b, err := New(
		Config{
			ManifestPath: manifestPath,
			OutputDir:    filepath.Join(tmpDir, "output"),
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Cancel context before Build
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = b.Build(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
