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

		for _, version := range provider.Versions {
			result.VersionCount++

			// Check version directory exists
			versionDir := filepath.Join(
				v.mirrorDir,
				provider.Hostname,
				provider.Namespace,
				provider.Name,
				version.Version,
			)
			if _, err := os.Stat(versionDir); os.IsNotExist(err) {
				result.Valid = false
				result.Errors = append(
					result.Errors,
					fmt.Sprintf("missing version directory: %s", versionDir),
				)
				continue
			}

			// Check SHA256SUMS file
			shasumsPath := filepath.Join(versionDir, "SHA256SUMS")
			shasumsData, err := os.ReadFile(shasumsPath)
			if err != nil {
				result.Valid = false
				result.Errors = append(
					result.Errors,
					fmt.Sprintf("cannot read SHA256SUMS: %s", shasumsPath),
				)
				continue
			}

			shasums := parseSHA256SUMS(string(shasumsData))

			// Verify each platform
			for _, platform := range version.Platforms {
				result.FileCount++

				filePath := filepath.Join(versionDir, platform.Filename)

				// Check file exists
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

				// Verify checksum matches SHA256SUMS file
				if sumsChecksum, ok := shasums[platform.Filename]; ok {
					if sumsChecksum != actualSum {
						result.Valid = false
						result.Errors = append(
							result.Errors,
							fmt.Sprintf("SHA256SUMS mismatch for %s", filePath),
						)
					}
				}
			}
		}

		// Check index.json
		indexPath := filepath.Join(
			v.mirrorDir,
			provider.Hostname,
			provider.Namespace,
			provider.Name,
			"index.json",
		)
		indexData, err := os.ReadFile(indexPath)
		if err != nil {
			result.Valid = false
			result.Errors = append(
				result.Errors,
				fmt.Sprintf("cannot read index.json: %s", indexPath),
			)
			continue
		}

		var index mirror.IndexJSON
		if err := json.Unmarshal(indexData, &index); err != nil {
			result.Valid = false
			result.Errors = append(
				result.Errors,
				fmt.Sprintf("invalid index.json: %s: %v", indexPath, err),
			)
			continue
		}

		// Verify all versions in lock file are in index.json
		for _, version := range provider.Versions {
			if _, ok := index.Versions[version.Version]; !ok {
				result.Valid = false
				result.Errors = append(
					result.Errors, fmt.Sprintf(
						"version %s not in index.json for %s/%s/%s",
						version.Version, provider.Hostname, provider.Namespace, provider.Name,
					),
				)
			}
		}
	}

	return result, nil
}

// parseSHA256SUMS parses a SHA256SUMS file
func parseSHA256SUMS(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "checksum  filename" (two spaces)
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) == 2 {
			result[parts[1]] = parts[0]
		}
	}
	return result
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
