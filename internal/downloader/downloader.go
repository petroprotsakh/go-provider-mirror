package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"

	"github.com/petroprotsakh/go-provider-mirror/internal/httpclient"
	"github.com/petroprotsakh/go-provider-mirror/internal/logging"
	"github.com/petroprotsakh/go-provider-mirror/internal/registry"
	"github.com/petroprotsakh/go-provider-mirror/internal/resolver"
)

// Config configures the downloader behavior.
type Config struct {
	CacheDir     string
	NoCache      bool
	Concurrency  int
	Retries      int
	MaxBackoff   time.Duration
	ShowProgress bool
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		CacheDir:     filepath.Join(os.TempDir(), "provider-mirror-cache"),
		Concurrency:  8,
		Retries:      3,
		MaxBackoff:   60 * time.Second,
		ShowProgress: true,
	}
}

// Downloader handles downloading provider binaries.
type Downloader struct {
	config     Config
	client     *registry.Client
	httpClient *httpclient.Client
	log        *logging.Logger
}

// New creates a new downloader.
func New(config Config, client *registry.Client) *Downloader {
	defaults := DefaultConfig()
	if config.CacheDir == "" {
		config.CacheDir = defaults.CacheDir
	}
	if config.Concurrency <= 0 {
		config.Concurrency = defaults.Concurrency
	}
	if config.Retries <= 0 {
		config.Retries = defaults.Retries
	}
	if config.MaxBackoff <= 0 {
		config.MaxBackoff = defaults.MaxBackoff
	}

	return &Downloader{
		config: config,
		client: client,
		httpClient: httpclient.New(
			httpclient.Config{
				Timeout: 5 * time.Minute, // longer timeout for downloads
			},
		),
		log: logging.Default(),
	}
}

// DownloadTask represents a single download task.
type DownloadTask struct {
	Provider resolver.ResolvedProvider
	Version  resolver.ResolvedVersion
	Platform string
	OS       string
	Arch     string
}

// Name returns a human-readable name for the task.
func (t DownloadTask) Name() string {
	return fmt.Sprintf(
		"%s/%s@%s %s",
		t.Provider.Source.Namespace,
		t.Provider.Source.Name,
		t.Version.Version,
		t.Platform,
	)
}

// DownloadResult represents the result of a download task.
type DownloadResult struct {
	Task        DownloadTask
	CachePath   string
	DownloadURL string
	Filename    string
	SHA256Sum   string
	Error       error
	FromCache   bool
}

// Download downloads all providers from the resolution.
func (d *Downloader) Download(
	ctx context.Context,
	resolution *resolver.Resolution,
) ([]DownloadResult, error) {
	if err := os.MkdirAll(d.config.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	// Build task list
	var tasks []DownloadTask
	for _, p := range resolution.Providers {
		for _, v := range p.Versions {
			for _, platform := range v.Platforms {
				osName, arch, err := registry.ParsePlatform(platform)
				if err != nil {
					return nil, fmt.Errorf("parsing platform %s: %w", platform, err)
				}
				tasks = append(
					tasks, DownloadTask{
						Provider: p,
						Version:  v,
						Platform: platform,
						OS:       osName,
						Arch:     arch,
					},
				)
			}
		}
	}

	d.log.Debug(
		"starting downloads",
		"total_tasks", len(tasks),
		"concurrency", d.config.Concurrency,
		"cache_dir", d.config.CacheDir,
		"no_cache", d.config.NoCache,
	)

	return d.downloadAll(ctx, tasks)
}

// downloadAll executes all download tasks concurrently.
func (d *Downloader) downloadAll(ctx context.Context, tasks []DownloadTask) (
	[]DownloadResult,
	error,
) {
	var progress *mpb.Progress
	var totalBar *mpb.Bar

	if d.config.ShowProgress {
		progress = mpb.NewWithContext(
			ctx,
			mpb.WithWidth(60),
			mpb.WithRefreshRate(100*time.Millisecond),
		)
		totalBar = progress.AddBar(
			int64(len(tasks)),
			mpb.PrependDecorators(
				decor.Name("Total", decor.WCSyncSpaceR),
				decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
			),
			mpb.AppendDecorators(
				decor.Percentage(decor.WCSyncSpace),
			),
			mpb.BarFillerClearOnComplete(),
		)
	}

	results := make([]DownloadResult, len(tasks))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, d.config.Concurrency)
	var mu sync.Mutex
	var firstError error

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t DownloadTask) {
			defer wg.Done()
			if totalBar != nil {
				defer totalBar.Increment()
			}

			select {
			case <-ctx.Done():
				results[idx] = DownloadResult{Task: t, Error: ctx.Err()}
				return
			default:
			}

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			select {
			case <-ctx.Done():
				results[idx] = DownloadResult{Task: t, Error: ctx.Err()}
				return
			default:
			}

			result := d.downloadTask(ctx, t, progress)
			results[idx] = result

			if result.Error != nil {
				mu.Lock()
				if firstError == nil {
					firstError = result.Error
				}
				mu.Unlock()
			} else if !d.config.ShowProgress {
				status := "downloaded"
				if result.FromCache {
					status = "cached"
				}
				d.log.Verbose(
					"file ready",
					"provider", t.Provider.Source.String(),
					"version", t.Version.Version,
					"platform", t.Platform,
					"status", status,
				)
			}
		}(i, task)
	}

	wg.Wait()
	if progress != nil {
		progress.Wait()
	}

	if firstError != nil {
		return results, fmt.Errorf("download failed: %w", firstError)
	}

	return results, nil
}

// downloadTask downloads a single provider binary.
func (d *Downloader) downloadTask(
	ctx context.Context,
	task DownloadTask,
	progress *mpb.Progress,
) DownloadResult {
	result := DownloadResult{Task: task}

	d.log.Debug(
		"fetching download info",
		"hostname", task.Provider.Source.Hostname,
		"namespace", task.Provider.Source.Namespace,
		"name", task.Provider.Source.Name,
		"version", task.Version.Version,
		"os", task.OS,
		"arch", task.Arch,
	)

	info, err := d.client.GetDownloadInfo(
		ctx,
		task.Provider.Source.Hostname,
		task.Provider.Source.Namespace,
		task.Provider.Source.Name,
		task.Version.Version,
		task.OS,
		task.Arch,
	)
	if err != nil {
		result.Error = fmt.Errorf("getting download info: %w", err)
		return result
	}

	result.DownloadURL = info.DownloadURL
	result.Filename = info.Filename
	result.SHA256Sum = info.SHA256Sum

	cachePath := d.cachePath(task, info.Filename)
	if d.checkCache(cachePath, info.SHA256Sum) {
		d.log.Debug("cache hit", "path", cachePath)
		result.CachePath = cachePath
		result.FromCache = true
		return result
	}

	d.log.Debug("cache miss, downloading", "url", info.DownloadURL, "dest", cachePath)

	if err := d.downloadWithRetry(
		ctx,
		info.DownloadURL,
		cachePath,
		info.SHA256Sum,
		task.Name(),
		progress,
	); err != nil {
		result.Error = err
		return result
	}

	result.CachePath = cachePath
	return result
}

// cachePath returns the cache path for a download.
func (d *Downloader) cachePath(task DownloadTask, filename string) string {
	return filepath.Join(
		d.config.CacheDir,
		task.Provider.Source.Hostname,
		task.Provider.Source.Namespace,
		task.Provider.Source.Name,
		task.Version.Version,
		task.Platform,
		filename,
	)
}

// checkCache checks if a file exists in cache and has the correct checksum.
func (d *Downloader) checkCache(path, expectedSHA256 string) bool {
	if d.config.NoCache {
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}

	return hex.EncodeToString(h.Sum(nil)) == expectedSHA256
}

// downloadWithRetry downloads a file with retry logic.
func (d *Downloader) downloadWithRetry(
	ctx context.Context,
	url, destPath, expectedSHA256, name string,
	progress *mpb.Progress,
) error {
	var lastErr error

	for attempt := 0; attempt <= d.config.Retries; attempt++ {
		if attempt > 0 {
			backoff := httpclient.Backoff(attempt, d.config.MaxBackoff, lastErr)
			d.log.Debug(
				"retrying download",
				"attempt", attempt+1,
				"max_attempts", d.config.Retries+1,
				"backoff", backoff,
				"url", url,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := d.downloadFile(ctx, url, destPath, expectedSHA256, name, progress)
		if err == nil {
			return nil
		}

		lastErr = fmt.Errorf("attempt %d/%d: %w", attempt+1, d.config.Retries+1, err)

		// Only retry if explicitly marked as retryable
		var re *httpclient.RetryableError
		if !errors.As(err, &re) {
			return lastErr
		}
	}

	return lastErr
}

// downloadFile downloads a single file with optional progress bar.
func (d *Downloader) downloadFile(
	ctx context.Context,
	url, destPath, expectedSHA256, name string,
	progress *mpb.Progress,
) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	// Cleanup on error
	success := false
	defer func() {
		_ = f.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	// Use shared client (adds User-Agent)
	resp, err := d.httpClient.Do(req)
	if err != nil {
		// Network errors are retryable
		return &httpclient.RetryableError{Err: fmt.Errorf("downloading: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return httpclient.NewHTTPError(resp)
	}

	// Set up reader (with or without progress bar)
	var reader io.Reader = resp.Body
	var bar *mpb.Bar

	if progress != nil {
		size := resp.ContentLength
		if size <= 0 {
			size = 1
		}

		displayName := name
		if len(displayName) > 35 {
			displayName = displayName[:32] + "..."
		}

		bar = progress.AddBar(
			size,
			mpb.PrependDecorators(
				decor.Name(displayName, decor.WCSyncSpaceR),
			),
			mpb.AppendDecorators(
				decor.CountersKibiByte("% .1f / % .1f"),
				decor.Name(" "),
				decor.AverageSpeed(decor.SizeB1024(0), "% .1f", decor.WCSyncSpace),
			),
			mpb.BarRemoveOnComplete(),
		)
		reader = bar.ProxyReader(resp.Body)
	}

	// Download and hash simultaneously
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), reader); err != nil {
		if bar != nil {
			bar.Abort(true)
		}
		return fmt.Errorf("writing file: %w", err)
	}

	// Verify checksum (not retryable - data corruption)
	actualSum := hex.EncodeToString(h.Sum(nil))
	if actualSum != expectedSHA256 {
		if bar != nil {
			bar.Abort(true)
		}
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, actualSum)
	}

	if err = f.Close(); err != nil {
		return fmt.Errorf("closing file: %w", err)
	}

	if err = os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("moving file: %w", err)
	}

	success = true
	return nil
}
