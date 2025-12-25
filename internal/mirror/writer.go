package mirror

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/petroprotsakh/go-provider-mirror/internal/downloader"
	"github.com/petroprotsakh/go-provider-mirror/internal/manifest"
)

// Writer writes provider mirror filesystem layout
type Writer struct {
	outputDir  string
	stagingDir string
}

// NewWriter creates a new mirror writer
func NewWriter(outputDir string) *Writer {
	return &Writer{
		outputDir:  outputDir,
		stagingDir: outputDir + ".staging",
	}
}

// IndexJSON represents the index.json file format for a provider
type IndexJSON struct {
	Versions map[string]interface{} `json:"versions"`
}

// Write writes the complete mirror from download results
func (w *Writer) Write(results []downloader.DownloadResult, m *manifest.Manifest) error {
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
	if err := w.writeLockFile(results, m); err != nil {
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

// writeProvider writes a single provider to the staging directory
func (w *Writer) writeProvider(
	hostname, namespace, name string,
	versions map[string][]downloader.DownloadResult,
) error {
	providerDir := filepath.Join(w.stagingDir, hostname, namespace, name)

	// Build index.json
	index := IndexJSON{
		Versions: make(map[string]interface{}),
	}

	for version, downloads := range versions {
		index.Versions[version] = struct{}{}

		versionDir := filepath.Join(providerDir, version)
		if err := os.MkdirAll(versionDir, 0o755); err != nil {
			return fmt.Errorf("creating version directory: %w", err)
		}

		// Write SHA256SUMS
		var shasums string
		for _, dl := range downloads {
			shasums += fmt.Sprintf("%s  %s\n", dl.SHA256Sum, dl.Filename)

			// Copy provider zip
			if err := copyFile(dl.CachePath, filepath.Join(versionDir, dl.Filename)); err != nil {
				return fmt.Errorf("copying %s: %w", dl.Filename, err)
			}
		}

		shasumsPath := filepath.Join(versionDir, "SHA256SUMS")
		if err := os.WriteFile(shasumsPath, []byte(shasums), 0o644); err != nil {
			return fmt.Errorf("writing SHA256SUMS: %w", err)
		}
	}

	// Write index.json
	indexPath := filepath.Join(providerDir, "index.json")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return fmt.Errorf("creating provider directory: %w", err)
	}

	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling index.json: %w", err)
	}

	if err := os.WriteFile(indexPath, indexData, 0o644); err != nil {
		return fmt.Errorf("writing index.json: %w", err)
	}

	return nil
}

// LockFile represents the mirror.lock file
type LockFile struct {
	Version     int                `json:"version"`
	GeneratedAt string             `json:"generated_at"`
	Engines     []string           `json:"engines"`
	Providers   []LockFileProvider `json:"providers"`
}

// LockFileProvider represents a provider in the lock file
type LockFileProvider struct {
	Source    string            `json:"source"`
	Hostname  string            `json:"hostname"`
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Versions  []LockFileVersion `json:"versions"`
}

// LockFileVersion represents a version in the lock file
type LockFileVersion struct {
	Version   string             `json:"version"`
	Platforms []LockFilePlatform `json:"platforms"`
}

// LockFilePlatform represents a platform in the lock file
type LockFilePlatform struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
}

// writeLockFile writes the mirror.lock file
func (w *Writer) writeLockFile(results []downloader.DownloadResult, m *manifest.Manifest) error {
	// Group results by provider
	type providerKey struct {
		source    string
		hostname  string
		namespace string
		name      string
	}

	providerMap := make(map[providerKey]*LockFileProvider)
	versionMap := make(map[string]map[string]*LockFileVersion) // provider -> version -> data

	for _, r := range results {
		pk := providerKey{
			source:    r.Task.Provider.SourceString,
			hostname:  r.Task.Provider.Source.Hostname,
			namespace: r.Task.Provider.Source.Namespace,
			name:      r.Task.Provider.Source.Name,
		}

		key := fmt.Sprintf("%s/%s/%s", pk.hostname, pk.namespace, pk.name)

		if providerMap[pk] == nil {
			providerMap[pk] = &LockFileProvider{
				Source:    pk.source,
				Hostname:  pk.hostname,
				Namespace: pk.namespace,
				Name:      pk.name,
			}
			versionMap[key] = make(map[string]*LockFileVersion)
		}

		if versionMap[key][r.Task.Version.Version] == nil {
			versionMap[key][r.Task.Version.Version] = &LockFileVersion{
				Version: r.Task.Version.Version,
			}
		}

		versionMap[key][r.Task.Version.Version].Platforms = append(
			versionMap[key][r.Task.Version.Version].Platforms,
			LockFilePlatform{
				OS:       r.Task.OS,
				Arch:     r.Task.Arch,
				Filename: r.Filename,
				SHA256:   r.SHA256Sum,
			},
		)
	}

	// Convert engines to strings
	engines := make([]string, len(m.Defaults.Engines))
	for i, e := range m.Defaults.Engines {
		engines[i] = string(e)
	}
	sort.Strings(engines)

	// Build lock file with stable ordering
	lockFile := LockFile{
		Version:     1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Engines:     engines,
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
		key := fmt.Sprintf("%s/%s/%s", pk.hostname, pk.namespace, pk.name)

		// Sort versions
		var versions []string
		for v := range versionMap[key] {
			versions = append(versions, v)
		}
		sort.Strings(versions)

		for _, v := range versions {
			lv := versionMap[key][v]
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
