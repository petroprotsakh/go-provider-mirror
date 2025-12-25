package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/petroprotsakh/go-provider-mirror/internal/downloader"
	"github.com/petroprotsakh/go-provider-mirror/internal/logging"
	"github.com/petroprotsakh/go-provider-mirror/internal/manifest"
	"github.com/petroprotsakh/go-provider-mirror/internal/mirror"
	"github.com/petroprotsakh/go-provider-mirror/internal/registry"
	"github.com/petroprotsakh/go-provider-mirror/internal/resolver"
)

type Config struct {
	ManifestPath string
	OutputDir    string
	CacheDir     string
	NoCache      bool
	Concurrency  int
	Retries      int
	MaxBackoff   int // seconds
}

type Builder struct {
	config   Config
	manifest *manifest.Manifest
	client   *registry.Client
	log      *logging.Logger
}

// New creates a new builder
func New(config Config) (*Builder, error) {
	m, err := manifest.Load(config.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	return &Builder{
		config:   config,
		manifest: m,
		client:   registry.NewClient(),
		log:      logging.Default(),
	}, nil
}

// Build executes the complete build process
func (b *Builder) Build(ctx context.Context) error {
	log := b.log

	// Header info
	if log.IsNormal() {
		log.Print("Building mirror from %s\n", b.config.ManifestPath)
		log.Print("Output directory: %s\n", b.config.OutputDir)
		log.Print("Providers: %d\n", len(b.manifest.Providers))
		log.Println()
	} else {
		logging.Info(
			"starting mirror build",
			"manifest", b.config.ManifestPath,
			"output", b.config.OutputDir,
			"providers", len(b.manifest.Providers),
		)
	}

	// Phase 1: Plan - resolve versions
	if log.IsNormal() {
		log.Print("→ Resolving provider versions...\n")
	} else {
		logging.Info("resolving provider versions")
	}

	startResolve := time.Now()

	res := resolver.New(b.client)
	resolution, err := res.Resolve(ctx, b.manifest)
	if err != nil {
		return fmt.Errorf("resolving versions: %w", err)
	}

	totalVersions := 0
	totalDownloads := 0
	for _, p := range resolution.Providers {
		totalVersions += len(p.Versions)
		for _, v := range p.Versions {
			totalDownloads += len(v.Platforms)
		}
	}

	resolveTime := time.Since(startResolve).Round(time.Millisecond)
	if log.IsNormal() {
		log.Print(
			"  Resolved %d provider(s), %d version(s) in %s\n",
			len(resolution.Providers), totalVersions, resolveTime,
		)
		log.Print("  Total downloads: %d\n", totalDownloads)
		log.Println()
	} else {
		logging.Info(
			"version resolution complete",
			"providers", len(resolution.Providers),
			"versions", totalVersions,
			"downloads", totalDownloads,
			"duration", resolveTime,
		)
	}

	// Log resolved versions in verbose mode
	for _, p := range resolution.Providers {
		for _, v := range p.Versions {
			logging.Verbose(
				"resolved version",
				"provider", p.Source.String(),
				"version", v.Version,
				"platforms", v.Platforms,
			)
		}
	}

	// Phase 2: Download
	if log.IsNormal() {
		log.Print("→ Downloading providers (%d files)...\n", totalDownloads)
	} else {
		logging.Info("downloading providers", "count", totalDownloads)
	}

	startDownload := time.Now()

	dl := downloader.New(
		downloader.Config{
			CacheDir:     b.config.CacheDir,
			NoCache:      b.config.NoCache,
			Concurrency:  b.config.Concurrency,
			Retries:      b.config.Retries,
			MaxBackoff:   time.Duration(b.config.MaxBackoff) * time.Second,
			ShowProgress: log.ShowProgress(),
		}, b.client,
	)

	results, err := dl.Download(ctx, resolution)

	// Count results
	var failures, fromCache, downloaded int
	for _, r := range results {
		if r.Error != nil {
			failures++
			if log.IsNormal() {
				log.Print("  ✗ %s: %v\n", r.Task.Provider.Source.String(), r.Error)
			} else {
				logging.Error(
					"download failed",
					"provider", r.Task.Provider.Source.String(),
					"version", r.Task.Version.Version,
					"platform", r.Task.Platform,
					"error", r.Error,
				)
			}
		} else if r.FromCache {
			fromCache++
		} else {
			downloaded++
		}
	}

	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	if failures > 0 {
		return fmt.Errorf("%d download(s) failed", failures)
	}

	downloadTime := time.Since(startDownload).Round(time.Millisecond)
	if log.IsNormal() {
		log.Print(
			"  Downloaded: %d, Cache hits: %d, Total: %d in %s\n",
			downloaded, fromCache, len(results), downloadTime,
		)
		log.Println()
	} else {
		logging.Info(
			"downloads complete",
			"downloaded", downloaded,
			"cache_hits", fromCache,
			"total", len(results),
			"duration", downloadTime,
		)
	}

	// Phase 3: Write mirror
	if log.IsNormal() {
		log.Print("→ Writing mirror...\n")
	} else {
		logging.Info("writing mirror")
	}

	startWrite := time.Now()

	writer := mirror.NewWriter(b.config.OutputDir)
	if err := writer.Write(results, b.manifest); err != nil {
		return fmt.Errorf("writing mirror: %w", err)
	}

	writeTime := time.Since(startWrite).Round(time.Millisecond)
	if log.IsNormal() {
		log.Print("  Wrote mirror in %s\n", writeTime)
		log.Println()
	} else {
		logging.Info("mirror written", "duration", writeTime)
	}

	// Summary
	if log.IsNormal() {
		log.Println("Mirror contents:")
		for _, p := range resolution.Providers {
			log.Print("  %s\n", p.Source.String())
			for _, v := range p.Versions {
				log.Print("    %s (%d platforms)\n", v.Version, len(v.Platforms))
			}
		}
		log.Println()
	} else {
		for _, p := range resolution.Providers {
			for _, v := range p.Versions {
				logging.Verbose(
					"mirror includes",
					"provider", p.Source.String(),
					"version", v.Version,
					"platforms", len(v.Platforms),
				)
			}
		}
		logging.Info(
			"build complete",
			"providers", len(resolution.Providers),
			"versions", totalVersions,
			"files", len(results),
			"total_duration", time.Since(startResolve).Round(time.Millisecond),
		)
	}

	return nil
}
