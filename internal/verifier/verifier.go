package verifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/petroprotsakh/go-provider-mirror/internal/mirror"
)

// Verifier validates provider mirror
type Verifier struct {
	mirrorDir string
}

// New creates a new verifier
func New(mirrorDir string) *Verifier {
	return &Verifier{
		mirrorDir: mirrorDir,
	}
}

// Result represents the verification result
type Result struct {
	Valid         bool
	Errors        []string
	ProviderCount int
	VersionCount  int
	FileCount     int
}

// Verify validates the mirror
func (v *Verifier) Verify(_ context.Context) (*Result, error) {
	result := &Result{Valid: true}

	// Check mirror directory exists
	if _, err := os.Stat(v.mirrorDir); os.IsNotExist(err) {
		result.Valid = false
		result.Errors = append(result.Errors, "mirror directory does not exist")
		return result, nil
	}

	// Check lock file exists
	lockPath := filepath.Join(v.mirrorDir, "mirror.lock")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("cannot read mirror.lock: %v", err))
		return result, nil
	}

	var lockFile mirror.LockFile
	if err := json.Unmarshal(lockData, &lockFile); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("invalid mirror.lock: %v", err))
		return result, nil
	}

	// Verify each provider
	for _, provider := range lockFile.Providers {
		result.ProviderCount++

		providerDir := filepath.Join(
			v.mirrorDir,
			provider.Hostname,
			provider.Namespace,
			provider.Name,
		)

		// Check index.json exists and is valid
		indexPath := filepath.Join(providerDir, "index.json")
		indexData, err := os.ReadFile(indexPath)
		if err != nil {
			result.Valid = false
			result.Errors = append(
				result.Errors,
				fmt.Sprintf(
					"cannot read index.json for %s/%s: %v",
					provider.Namespace, provider.Name, err,
				),
			)
		} else {
			var index mirror.IndexJSON
			if err := json.Unmarshal(indexData, &index); err != nil {
				result.Valid = false
				result.Errors = append(
					result.Errors,
					fmt.Sprintf(
						"invalid index.json for %s/%s: %v",
						provider.Namespace, provider.Name, err,
					),
				)
			} else {
				// Verify all versions in lock file are in index.json
				for _, version := range provider.Versions {
					if _, ok := index.Versions[version.Version]; !ok {
						result.Valid = false
						result.Errors = append(
							result.Errors,
							fmt.Sprintf(
								"version %s not in index.json for %s/%s",
								version.Version, provider.Namespace, provider.Name,
							),
						)
					}
				}
			}
		}

		for _, version := range provider.Versions {
			result.VersionCount++

			// Check <version>.json exists and is valid
			versionJSONPath := filepath.Join(providerDir, version.Version+".json")
			versionData, err := os.ReadFile(versionJSONPath)
			if err != nil {
				result.Valid = false
				result.Errors = append(
					result.Errors,
					fmt.Sprintf("cannot read %s.json: %v", version.Version, err),
				)
				continue
			}

			var versionMeta mirror.VersionJSON
			if err := json.Unmarshal(versionData, &versionMeta); err != nil {
				result.Valid = false
				result.Errors = append(
					result.Errors,
					fmt.Sprintf("invalid %s.json: %v", version.Version, err),
				)
				continue
			}

			// Verify each platform
			for _, platform := range version.Platforms {
				result.FileCount++

				platformKey := fmt.Sprintf("%s_%s", platform.OS, platform.Arch)

				// Check archive exists
				filePath := filepath.Join(providerDir, platform.Filename)
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("missing file: %s", filePath))
					continue
				}

				// Verify checksum from lock file
				actualSum, err := fileSHA256(filePath)
				if err != nil {
					result.Valid = false
					result.Errors = append(
						result.Errors,
						fmt.Sprintf("cannot read file: %s: %v", filePath, err),
					)
					continue
				}

				if actualSum != platform.SHA256 {
					result.Valid = false
					result.Errors = append(
						result.Errors, fmt.Sprintf(
							"checksum mismatch for %s: expected %s, got %s",
							filePath, platform.SHA256, actualSum,
						),
					)
					continue
				}

				// Verify version.json has this platform
				archiveInfo, ok := versionMeta.Archives[platformKey]
				if !ok {
					result.Valid = false
					result.Errors = append(
						result.Errors,
						fmt.Sprintf("platform %s not in %s.json", platformKey, version.Version),
					)
					continue
				}

				// Compute actual h1: hash from package contents
				actualH1, err := mirror.ComputePackageHash(filePath)
				if err != nil {
					result.Valid = false
					result.Errors = append(
						result.Errors,
						fmt.Sprintf("cannot compute h1 hash for %s: %v", filePath, err),
					)
					continue
				}

				// Verify h1 hash in version.json matches computed hash
				if !containsHash(archiveInfo.Hashes, actualH1) {
					result.Valid = false
					result.Errors = append(
						result.Errors,
						fmt.Sprintf(
							"h1 hash mismatch in %s.json for %s: expected %s, got %v",
							version.Version, platformKey, actualH1, archiveInfo.Hashes,
						),
					)
				}

				// Verify URL in version.json matches filename
				if archiveInfo.URL != platform.Filename {
					result.Valid = false
					result.Errors = append(
						result.Errors,
						fmt.Sprintf(
							"URL mismatch in %s.json for %s: expected %s, got %s",
							version.Version, platformKey, platform.Filename, archiveInfo.URL,
						),
					)
				}
			}
		}
	}

	return result, nil
}

// containsHash checks if a hash is in the list
func containsHash(hashes []string, target string) bool {
	for _, h := range hashes {
		if strings.EqualFold(h, target) {
			return true
		}
	}
	return false
}

// fileSHA256 calculates the SHA256 hash of a file
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
