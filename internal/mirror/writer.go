package mirror

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/mod/sumdb/dirhash"

	"github.com/petroprotsakh/go-provider-mirror/internal/downloader"
)

// Writer writes provider mirror filesystem layout
type Writer struct {
	outputDir  string
	stagingDir string
}

// NewWriter creates a new mirror writer
func NewWriter(outputDir string) *Writer {
	outputDir = filepath.Clean(outputDir)
	return &Writer{
		outputDir:  outputDir,
		stagingDir: outputDir + ".staging",
	}
}

// IndexJSON represents the index.json file listing available versions.
type IndexJSON struct {
	Versions map[string]struct{} `json:"versions"`
}

// VersionJSON represents the <version>.json file format for a provider version.
type VersionJSON struct {
	Archives map[string]ArchiveInfo `json:"archives"`
}

// ArchiveInfo represents a single platform archive in the version metadata.
type ArchiveInfo struct {
	Hashes []string `json:"hashes"`
	URL    string   `json:"url"`
}

// Write writes the complete mirror from download results
func (w *Writer) Write(results []downloader.DownloadResult) error {
	// Clean staging directory
	if err := os.RemoveAll(w.stagingDir); err != nil {
		return fmt.Errorf("cleaning staging directory: %w", err)
	}

	// Group results by provider and version
	type providerKey struct {
		hostname  string
		namespace string
		name      string
	}

	providerVersions := make(map[providerKey]map[string][]downloader.DownloadResult)

	for _, r := range results {
		if r.Error != nil {
			return fmt.Errorf(
				"cannot write mirror: download failed for %s: %w",
				r.Task.Provider.Source.String(), r.Error,
			)
		}

		pk := providerKey{
			hostname:  r.Task.Provider.Source.Hostname,
			namespace: r.Task.Provider.Source.Namespace,
			name:      r.Task.Provider.Source.Name,
		}

		if providerVersions[pk] == nil {
			providerVersions[pk] = make(map[string][]downloader.DownloadResult)
		}
		providerVersions[pk][r.Task.Version.Version] = append(
			providerVersions[pk][r.Task.Version.Version],
			r,
		)
	}

	// Write each provider
	for pk, versions := range providerVersions {
		if err := w.writeProvider(pk.hostname, pk.namespace, pk.name, versions); err != nil {
			return fmt.Errorf(
				"writing provider %s/%s/%s: %w",
				pk.hostname, pk.namespace, pk.name, err,
			)
		}
	}

	// Write lock file
	if err := w.writeLockFile(results); err != nil {
		return fmt.Errorf("writing lock file: %w", err)
	}

	// Atomic swap: remove old output, rename staging to output
	if err := os.RemoveAll(w.outputDir); err != nil {
		return fmt.Errorf("removing old output directory: %w", err)
	}

	if err := os.Rename(w.stagingDir, w.outputDir); err != nil {
		return fmt.Errorf("moving staging to output: %w", err)
	}

	return nil
}

// writeProvider writes a single provider to the staging directory.
func (w *Writer) writeProvider(
	hostname, namespace, name string,
	versions map[string][]downloader.DownloadResult,
) error {
	providerDir := filepath.Join(w.stagingDir, hostname, namespace, name)

	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		return fmt.Errorf("creating provider directory: %w", err)
	}

	// Build index.json with all versions
	index := IndexJSON{
		Versions: make(map[string]struct{}),
	}

	for version, downloads := range versions {
		// Add to index
		index.Versions[version] = struct{}{}

		// Build version metadata
		versionMeta := VersionJSON{
			Archives: make(map[string]ArchiveInfo),
		}

		for _, dl := range downloads {
			platform := fmt.Sprintf("%s_%s", dl.Task.OS, dl.Task.Arch)

			// Copy provider zip
			if err := copyFile(dl.CachePath, filepath.Join(providerDir, dl.Filename)); err != nil {
				return fmt.Errorf("copying %s: %w", dl.Filename, err)
			}

			// Compute h1: hash from extracted package contents
			h1Hash, err := ComputePackageHash(dl.CachePath)
			if err != nil {
				return fmt.Errorf("computing h1 hash for %s: %w", dl.Filename, err)
			}

			versionMeta.Archives[platform] = ArchiveInfo{
				Hashes: []string{h1Hash},
				URL:    dl.Filename, // relative path within provider directory
			}
		}

		// Write <version>.json
		versionPath := filepath.Join(providerDir, version+".json")
		versionData, err := json.MarshalIndent(versionMeta, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling %s.json: %w", version, err)
		}

		if err := os.WriteFile(versionPath, append(versionData, '\n'), 0o644); err != nil {
			return fmt.Errorf("writing %s.json: %w", version, err)
		}
	}

	// Write index.json
	indexPath := filepath.Join(providerDir, "index.json")
	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling index.json: %w", err)
	}

	if err := os.WriteFile(indexPath, append(indexData, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing index.json: %w", err)
	}

	return nil
}

// ComputePackageHash computes the h1: hash from a provider ZIP file content.
func ComputePackageHash(zipPath string) (string, error) {
	hash, err := dirhash.HashZip(zipPath, dirhash.Hash1)
	if err != nil {
		return "", fmt.Errorf("computing package hash: %w", err)
	}
	return hash, nil
}

// LockFile represents the mirror.lock file
type LockFile struct {
	Version     int                `json:"version"`
	GeneratedAt string             `json:"generated_at"`
	Providers   []LockFileProvider `json:"providers"`
}

// LockFileProvider represents a provider in the lock file
type LockFileProvider struct {
	Hostname  string            `json:"hostname"`
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Versions  []LockFileVersion `json:"versions"`
}

// LockFileVersion represents a version in the lock file
type LockFileVersion struct {
	Version         string             `json:"version"`
	ManifestSources []string           `json:"manifest_sources"` // original source specs from manifest
	Platforms       []LockFilePlatform `json:"platforms"`
}

// LockFilePlatform represents a platform in the lock file
type LockFilePlatform struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"` // archive checksum (from registry)
	H1       string `json:"h1"`     // content hash (computed from package contents)
}

// writeLockFile writes the mirror.lock file
func (w *Writer) writeLockFile(results []downloader.DownloadResult) error {
	// Group results by provider
	type providerKey struct {
		hostname  string
		namespace string
		name      string
	}

	providerMap := make(map[providerKey]*LockFileProvider)
	versionMap := make(map[providerKey]map[string]*LockFileVersion) // provider -> version -> data

	for _, r := range results {
		pk := providerKey{
			hostname:  r.Task.Provider.Source.Hostname,
			namespace: r.Task.Provider.Source.Namespace,
			name:      r.Task.Provider.Source.Name,
		}

		if providerMap[pk] == nil {
			providerMap[pk] = &LockFileProvider{
				Hostname:  pk.hostname,
				Namespace: pk.namespace,
				Name:      pk.name,
			}
			versionMap[pk] = make(map[string]*LockFileVersion)
		}

		ver := r.Task.Version.Version
		if versionMap[pk][ver] == nil {
			versionMap[pk][ver] = &LockFileVersion{
				Version:         ver,
				ManifestSources: r.Task.Version.ManifestSources,
			}
		}

		// Compute h1 hash from package contents
		h1Hash, err := ComputePackageHash(r.CachePath)
		if err != nil {
			return fmt.Errorf("computing h1 hash for lock file: %w", err)
		}

		versionMap[pk][ver].Platforms = append(
			versionMap[pk][ver].Platforms,
			LockFilePlatform{
				OS:       r.Task.OS,
				Arch:     r.Task.Arch,
				Filename: r.Filename,
				SHA256:   r.SHA256Sum,
				H1:       h1Hash,
			},
		)
	}

	// Build lock file with stable ordering
	lockFile := LockFile{
		Version:     1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Sort providers for deterministic output
	var providerKeys []providerKey
	for pk := range providerMap {
		providerKeys = append(providerKeys, pk)
	}
	sort.Slice(
		providerKeys, func(i, j int) bool {
			if providerKeys[i].hostname != providerKeys[j].hostname {
				return providerKeys[i].hostname < providerKeys[j].hostname
			}
			if providerKeys[i].namespace != providerKeys[j].namespace {
				return providerKeys[i].namespace < providerKeys[j].namespace
			}
			return providerKeys[i].name < providerKeys[j].name
		},
	)

	for _, pk := range providerKeys {
		provider := providerMap[pk]

		// Sort versions
		var versions []string
		for v := range versionMap[pk] {
			versions = append(versions, v)
		}
		sort.Strings(versions)

		for _, v := range versions {
			lv := versionMap[pk][v]
			// Sort platforms
			sort.Slice(
				lv.Platforms, func(i, j int) bool {
					if lv.Platforms[i].OS != lv.Platforms[j].OS {
						return lv.Platforms[i].OS < lv.Platforms[j].OS
					}
					return lv.Platforms[i].Arch < lv.Platforms[j].Arch
				},
			)
			provider.Versions = append(provider.Versions, *lv)
		}

		lockFile.Providers = append(lockFile.Providers, *provider)
	}

	// Write lock file
	lockData, err := json.MarshalIndent(lockFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling lock file: %w", err)
	}

	lockPath := filepath.Join(w.stagingDir, "mirror.lock")
	if err := os.WriteFile(lockPath, append(lockData, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing lock file: %w", err)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close() //nolint:errcheck

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return err
	}

	return dstFile.Close()
}
