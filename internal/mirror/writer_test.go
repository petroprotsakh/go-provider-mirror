package mirror

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- NewWriter tests ---

func TestNewWriter(t *testing.T) {
	w := NewWriter("/tmp/mirror")

	if w.outputDir != "/tmp/mirror" {
		t.Errorf("expected outputDir /tmp/mirror, got %s", w.outputDir)
	}

	if w.stagingDir != "/tmp/mirror.staging" {
		t.Errorf("expected stagingDir /tmp/mirror.staging, got %s", w.stagingDir)
	}
}

func TestNewWriter_TrailingSlash(t *testing.T) {
	w := NewWriter("mirror/")

	if w.outputDir != "mirror" {
		t.Errorf("expected outputDir 'mirror', got %s", w.outputDir)
	}

	if w.stagingDir != "mirror.staging" {
		t.Errorf("expected stagingDir 'mirror.staging', got %s", w.stagingDir)
	}
}

func TestNewWriter_DifferentPaths(t *testing.T) {
	tests := []struct {
		input       string
		wantOutput  string
		wantStaging string
	}{
		{"/tmp/mirror", "/tmp/mirror", "/tmp/mirror.staging"},
		{"./output", "output", "output.staging"},
		{"/var/data/providers", "/var/data/providers", "/var/data/providers.staging"},
		{"/tmp/mirror/", "/tmp/mirror", "/tmp/mirror.staging"}, // trailing slash
		{"mirror/", "mirror", "mirror.staging"},                // trailing slash
		{"./mirror/", "mirror", "mirror.staging"},              // ./ and trailing slash
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			w := NewWriter(tt.input)

			if w.outputDir != tt.wantOutput {
				t.Errorf("outputDir = %s, want %s", w.outputDir, tt.wantOutput)
			}

			if w.stagingDir != tt.wantStaging {
				t.Errorf("stagingDir = %s, want %s", w.stagingDir, tt.wantStaging)
			}
		})
	}
}

// --- ComputePackageHash tests ---

func TestComputePackageHash_ValidZip(t *testing.T) {
	// Create a temporary ZIP file with known content
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")

	if err := createTestZip(zipPath, map[string]string{
		"terraform-provider-null_v3.2.4_x5": "binary content here",
	}); err != nil {
		t.Fatalf("failed to create test zip: %v", err)
	}

	hash, err := ComputePackageHash(zipPath)
	if err != nil {
		t.Fatalf("ComputePackageHash() error = %v", err)
	}

	// Should return h1: prefixed hash
	if !strings.HasPrefix(hash, "h1:") {
		t.Errorf("hash should start with 'h1:', got %s", hash)
	}

	// Hash should be base64 encoded (44 chars after h1:)
	if len(hash) != 47 { // "h1:" + 44 base64 chars
		t.Errorf("hash length should be 47, got %d", len(hash))
	}
}

func TestComputePackageHash_DeterministicForSameContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two identical ZIPs
	content := map[string]string{
		"terraform-provider-null_v3.2.4_x5": "test content",
	}

	zip1 := filepath.Join(tmpDir, "test1.zip")
	zip2 := filepath.Join(tmpDir, "test2.zip")

	if err := createTestZip(zip1, content); err != nil {
		t.Fatalf("failed to create zip1: %v", err)
	}
	if err := createTestZip(zip2, content); err != nil {
		t.Fatalf("failed to create zip2: %v", err)
	}

	hash1, err := ComputePackageHash(zip1)
	if err != nil {
		t.Fatalf("hash1 error: %v", err)
	}

	hash2, err := ComputePackageHash(zip2)
	if err != nil {
		t.Fatalf("hash2 error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("hashes should be identical for same content: %s != %s", hash1, hash2)
	}
}

func TestComputePackageHash_DifferentContentDifferentHash(t *testing.T) {
	tmpDir := t.TempDir()

	zip1 := filepath.Join(tmpDir, "test1.zip")
	zip2 := filepath.Join(tmpDir, "test2.zip")

	if err := createTestZip(zip1, map[string]string{"file": "content1"}); err != nil {
		t.Fatalf("failed to create zip1: %v", err)
	}
	if err := createTestZip(zip2, map[string]string{"file": "content2"}); err != nil {
		t.Fatalf("failed to create zip2: %v", err)
	}

	hash1, err := ComputePackageHash(zip1)
	if err != nil {
		t.Fatalf("hash1 error: %v", err)
	}

	hash2, err := ComputePackageHash(zip2)
	if err != nil {
		t.Fatalf("hash2 error: %v", err)
	}

	if hash1 == hash2 {
		t.Error("hashes should be different for different content")
	}
}

func TestComputePackageHash_NonexistentFile(t *testing.T) {
	_, err := ComputePackageHash("/nonexistent/path.zip")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestComputePackageHash_InvalidZip(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "invalid.zip")

	// Write invalid content
	if err := os.WriteFile(invalidPath, []byte("not a zip file"), 0644); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}

	_, err := ComputePackageHash(invalidPath)
	if err == nil {
		t.Error("expected error for invalid zip")
	}
}

// --- JSON structure tests ---

func TestIndexJSON_Marshal(t *testing.T) {
	index := IndexJSON{
		Versions: map[string]struct{}{
			"3.2.4": {},
			"3.2.3": {},
		},
	}

	data, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify it's valid JSON
	var result IndexJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(result.Versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(result.Versions))
	}
}

func TestVersionJSON_Marshal(t *testing.T) {
	version := VersionJSON{
		Archives: map[string]ArchiveInfo{
			"linux_amd64": {
				Hashes: []string{"h1:abc123"},
				URL:    "terraform-provider-null_3.2.4_linux_amd64.zip",
			},
		},
	}

	data, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var result VersionJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(result.Archives) != 1 {
		t.Errorf("expected 1 archive, got %d", len(result.Archives))
	}

	arch := result.Archives["linux_amd64"]
	if len(arch.Hashes) != 1 || arch.Hashes[0] != "h1:abc123" {
		t.Errorf("unexpected hashes: %v", arch.Hashes)
	}
}

func TestArchiveInfo_Fields(t *testing.T) {
	info := ArchiveInfo{
		Hashes: []string{"h1:abc123", "zh:def456"},
		URL:    "provider.zip",
	}

	if len(info.Hashes) != 2 {
		t.Errorf("expected 2 hashes, got %d", len(info.Hashes))
	}

	if info.URL != "provider.zip" {
		t.Errorf("expected URL 'provider.zip', got %s", info.URL)
	}
}

// --- LockFile structure tests ---

func TestLockFile_Marshal(t *testing.T) {
	lockFile := LockFile{
		Version:     1,
		GeneratedAt: "2024-01-01T00:00:00Z",
		Providers: []LockFileProvider{
			{
				Hostname:  "registry.terraform.io",
				Namespace: "hashicorp",
				Name:      "null",
				Versions: []LockFileVersion{
					{
						Version:         "3.2.4",
						ManifestSources: []string{"hashicorp/null"},
						Platforms: []LockFilePlatform{
							{
								OS:       "linux",
								Arch:     "amd64",
								Filename: "terraform-provider-null_3.2.4_linux_amd64.zip",
								SHA256:   "abc123",
								H1:       "h1:xyz789",
							},
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(lockFile, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify structure
	var result LockFile
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if result.Version != 1 {
		t.Errorf("expected version 1, got %d", result.Version)
	}

	if len(result.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(result.Providers))
	}

	provider := result.Providers[0]
	if provider.Hostname != "registry.terraform.io" {
		t.Errorf("unexpected hostname: %s", provider.Hostname)
	}

	if len(provider.Versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(provider.Versions))
	}

	version := provider.Versions[0]
	if version.Version != "3.2.4" {
		t.Errorf("unexpected version: %s", version.Version)
	}

	if len(version.ManifestSources) != 1 || version.ManifestSources[0] != "hashicorp/null" {
		t.Errorf("unexpected manifest sources: %v", version.ManifestSources)
	}

	if len(version.Platforms) != 1 {
		t.Fatalf("expected 1 platform, got %d", len(version.Platforms))
	}

	platform := version.Platforms[0]
	if platform.OS != "linux" || platform.Arch != "amd64" {
		t.Errorf("unexpected platform: %s_%s", platform.OS, platform.Arch)
	}
	if platform.SHA256 != "abc123" {
		t.Errorf("unexpected SHA256: %s", platform.SHA256)
	}
	if platform.H1 != "h1:xyz789" {
		t.Errorf("unexpected H1: %s", platform.H1)
	}
}

func TestLockFilePlatform_Fields(t *testing.T) {
	platform := LockFilePlatform{
		OS:       "darwin",
		Arch:     "arm64",
		Filename: "terraform-provider-null_3.2.4_darwin_arm64.zip",
		SHA256:   "abcdef123456",
		H1:       "h1:base64hash",
	}

	if platform.OS != "darwin" {
		t.Errorf("unexpected OS: %s", platform.OS)
	}

	if platform.Arch != "arm64" {
		t.Errorf("unexpected Arch: %s", platform.Arch)
	}

	if platform.SHA256 == "" {
		t.Error("SHA256 should not be empty")
	}

	if !strings.HasPrefix(platform.H1, "h1:") {
		t.Errorf("H1 should start with 'h1:', got %s", platform.H1)
	}
}

// --- copyFile tests ---

func TestCopyFile_Success(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "source.txt")
	dst := filepath.Join(tmpDir, "dest.txt")

	content := []byte("test content for copying")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	// Verify content
	result, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read dest: %v", err)
	}

	if string(result) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", result, content)
	}
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	err := copyFile("/nonexistent/file", filepath.Join(tmpDir, "dest"))
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestCopyFile_DestDirNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "source.txt")
	if err := os.WriteFile(src, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	err := copyFile(src, "/nonexistent/dir/dest.txt")
	if err == nil {
		t.Error("expected error for nonexistent destination directory")
	}
}

func TestCopyFile_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "large.bin")
	dst := filepath.Join(tmpDir, "large_copy.bin")

	// Create a 1MB file
	content := make([]byte, 1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}

	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	// Verify size
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("failed to stat dest: %v", err)
	}

	if info.Size() != int64(len(content)) {
		t.Errorf("size mismatch: got %d, want %d", info.Size(), len(content))
	}
}

// --- Helper functions ---

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
