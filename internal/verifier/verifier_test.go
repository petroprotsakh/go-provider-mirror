package verifier

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/petroprotsakh/go-provider-mirror/internal/mirror"
)

// --- New tests ---

func TestNew(t *testing.T) {
	v := New("/tmp/mirror")

	if v.mirrorDir != "/tmp/mirror" {
		t.Errorf("expected mirrorDir /tmp/mirror, got %s", v.mirrorDir)
	}
}

// --- Result tests ---

func TestResult_Fields(t *testing.T) {
	r := &Result{
		Valid:         true,
		Errors:        nil,
		ProviderCount: 2,
		VersionCount:  4,
		FileCount:     8,
	}

	if !r.Valid {
		t.Error("expected Valid to be true")
	}

	if r.ProviderCount != 2 {
		t.Errorf("expected ProviderCount 2, got %d", r.ProviderCount)
	}

	if r.VersionCount != 4 {
		t.Errorf("expected VersionCount 4, got %d", r.VersionCount)
	}

	if r.FileCount != 8 {
		t.Errorf("expected FileCount 8, got %d", r.FileCount)
	}
}

// --- Verify tests ---

func TestVerify_MissingMirrorDir(t *testing.T) {
	v := New("/nonexistent/path")

	result, err := v.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if result.Valid {
		t.Error("expected Valid to be false for missing directory")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestVerify_MissingLockFile(t *testing.T) {
	tmpDir := t.TempDir()

	v := New(tmpDir)

	result, err := v.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if result.Valid {
		t.Error("expected Valid to be false for missing lock file")
	}
}

func TestVerify_InvalidLockFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Write invalid JSON
	lockPath := filepath.Join(tmpDir, "mirror.lock")
	if err := os.WriteFile(lockPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}

	v := New(tmpDir)

	result, err := v.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if result.Valid {
		t.Error("expected Valid to be false for invalid lock file")
	}
}

func TestVerify_ValidMirror(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid mirror structure
	if err := createValidMirror(tmpDir); err != nil {
		t.Fatalf("failed to create valid mirror: %v", err)
	}

	v := New(tmpDir)

	result, err := v.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if !result.Valid {
		t.Errorf("expected Valid to be true, errors: %v", result.Errors)
	}

	if result.ProviderCount != 1 {
		t.Errorf("expected ProviderCount 1, got %d", result.ProviderCount)
	}

	if result.VersionCount != 1 {
		t.Errorf("expected VersionCount 1, got %d", result.VersionCount)
	}

	if result.FileCount != 1 {
		t.Errorf("expected FileCount 1, got %d", result.FileCount)
	}
}

func TestVerify_MissingProviderDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create lock file but no provider directory
	lockFile := mirror.LockFile{
		Version:     1,
		GeneratedAt: "2024-01-01T00:00:00Z",
		Providers: []mirror.LockFileProvider{
			{
				Hostname:  "registry.terraform.io",
				Namespace: "hashicorp",
				Name:      "null",
				Versions: []mirror.LockFileVersion{
					{
						Version: "3.2.4",
						Platforms: []mirror.LockFilePlatform{
							{OS: "linux", Arch: "amd64", Filename: "test.zip", SHA256: "abc123"},
						},
					},
				},
			},
		},
	}

	lockData, _ := json.MarshalIndent(lockFile, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "mirror.lock"), lockData, 0644); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}

	v := New(tmpDir)

	result, err := v.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if result.Valid {
		t.Error("expected Valid to be false for missing provider dir")
	}
}

func TestVerify_ChecksumMismatch(t *testing.T) {
	tmpDir := t.TempDir()

	// Create structure with wrong checksum
	providerDir := filepath.Join(tmpDir, "registry.terraform.io", "hashicorp", "null")
	if err := os.MkdirAll(providerDir, 0755); err != nil {
		t.Fatalf("failed to create provider dir: %v", err)
	}

	// Create zip file
	zipPath := filepath.Join(providerDir, "terraform-provider-null_3.2.4_linux_amd64.zip")
	if err := createTestZip(zipPath, map[string]string{"file": "content"}); err != nil {
		t.Fatalf("failed to create zip: %v", err)
	}

	// Compute actual hash for version.json
	actualH1, _ := mirror.ComputePackageHash(zipPath)

	// Create index.json
	index := mirror.IndexJSON{Versions: map[string]struct{}{"3.2.4": {}}}
	indexData, _ := json.MarshalIndent(index, "", "  ")
	if err := os.WriteFile(filepath.Join(providerDir, "index.json"), indexData, 0644); err != nil {
		t.Fatalf("failed to write index.json: %v", err)
	}

	// Create version.json with correct h1 hash
	versionMeta := mirror.VersionJSON{
		Archives: map[string]mirror.ArchiveInfo{
			"linux_amd64": {
				Hashes: []string{actualH1},
				URL:    "terraform-provider-null_3.2.4_linux_amd64.zip",
			},
		},
	}
	versionData, _ := json.MarshalIndent(versionMeta, "", "  ")
	if err := os.WriteFile(filepath.Join(providerDir, "3.2.4.json"), versionData, 0644); err != nil {
		t.Fatalf("failed to write version.json: %v", err)
	}

	// Create lock file with WRONG SHA256 checksum
	lockFile := mirror.LockFile{
		Version:     1,
		GeneratedAt: "2024-01-01T00:00:00Z",
		Providers: []mirror.LockFileProvider{
			{
				Hostname:  "registry.terraform.io",
				Namespace: "hashicorp",
				Name:      "null",
				Versions: []mirror.LockFileVersion{
					{
						Version: "3.2.4",
						Platforms: []mirror.LockFilePlatform{
							{
								OS:       "linux",
								Arch:     "amd64",
								Filename: "terraform-provider-null_3.2.4_linux_amd64.zip",
								SHA256:   "wrong_checksum_here", // Wrong!
								H1:       actualH1,
							},
						},
					},
				},
			},
		},
	}

	lockData, _ := json.MarshalIndent(lockFile, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "mirror.lock"), lockData, 0644); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}

	v := New(tmpDir)

	result, err := v.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if result.Valid {
		t.Error("expected Valid to be false for checksum mismatch")
	}

	// Should have checksum mismatch error
	found := false
	for _, e := range result.Errors {
		if contains(e, "checksum mismatch") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected checksum mismatch error, got: %v", result.Errors)
	}
}

// --- containsHash tests ---

func TestContainsHash(t *testing.T) {
	tests := []struct {
		name   string
		hashes []string
		target string
		want   bool
	}{
		{"found", []string{"h1:abc", "h1:def"}, "h1:abc", true},
		{"not found", []string{"h1:abc", "h1:def"}, "h1:xyz", false},
		{"empty list", []string{}, "h1:abc", false},
		{"case insensitive", []string{"H1:ABC"}, "h1:abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsHash(tt.hashes, tt.target); got != tt.want {
				t.Errorf("containsHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- fileSHA256 tests ---

func TestFileSHA256(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	content := []byte("test content for sha256")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	hash, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("fileSHA256() error = %v", err)
	}

	// Verify hash
	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])

	if hash != expected {
		t.Errorf("hash mismatch: got %s, want %s", hash, expected)
	}
}

func TestFileSHA256_FileNotFound(t *testing.T) {
	_, err := fileSHA256("/nonexistent/file")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestFileSHA256_Deterministic(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	content := []byte("same content")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	hash1, _ := fileSHA256(path)
	hash2, _ := fileSHA256(path)

	if hash1 != hash2 {
		t.Error("same file should produce same hash")
	}
}

// --- Helper functions ---

func createValidMirror(dir string) error {
	providerDir := filepath.Join(dir, "registry.terraform.io", "hashicorp", "null")
	if err := os.MkdirAll(providerDir, 0755); err != nil {
		return err
	}

	// Create zip file
	zipPath := filepath.Join(providerDir, "terraform-provider-null_3.2.4_linux_amd64.zip")
	if err := createTestZip(zipPath, map[string]string{
		"terraform-provider-null_v3.2.4_x5": "binary content",
	}); err != nil {
		return err
	}

	// Compute hashes
	sha256sum, err := fileSHA256(zipPath)
	if err != nil {
		return err
	}

	h1Hash, err := mirror.ComputePackageHash(zipPath)
	if err != nil {
		return err
	}

	// Create index.json
	index := mirror.IndexJSON{
		Versions: map[string]struct{}{
			"3.2.4": {},
		},
	}
	indexData, _ := json.MarshalIndent(index, "", "  ")
	if err := os.WriteFile(filepath.Join(providerDir, "index.json"), indexData, 0644); err != nil {
		return err
	}

	// Create version.json
	versionMeta := mirror.VersionJSON{
		Archives: map[string]mirror.ArchiveInfo{
			"linux_amd64": {
				Hashes: []string{h1Hash},
				URL:    "terraform-provider-null_3.2.4_linux_amd64.zip",
			},
		},
	}
	versionData, _ := json.MarshalIndent(versionMeta, "", "  ")
	if err := os.WriteFile(filepath.Join(providerDir, "3.2.4.json"), versionData, 0644); err != nil {
		return err
	}

	// Create mirror.lock
	lockFile := mirror.LockFile{
		Version:     1,
		GeneratedAt: "2024-01-01T00:00:00Z",
		Providers: []mirror.LockFileProvider{
			{
				Hostname:  "registry.terraform.io",
				Namespace: "hashicorp",
				Name:      "null",
				Versions: []mirror.LockFileVersion{
					{
						Version:         "3.2.4",
						ManifestSources: []string{"hashicorp/null"},
						Platforms: []mirror.LockFilePlatform{
							{
								OS:       "linux",
								Arch:     "amd64",
								Filename: "terraform-provider-null_3.2.4_linux_amd64.zip",
								SHA256:   sha256sum,
								H1:       h1Hash,
							},
						},
					},
				},
			},
		},
	}
	lockData, _ := json.MarshalIndent(lockFile, "", "  ")
	return os.WriteFile(filepath.Join(dir, "mirror.lock"), lockData, 0644)
}

func createTestZip(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	w := zip.NewWriter(f)
	defer w.Close() //nolint:errcheck

	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			return err
		}
	}

	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
