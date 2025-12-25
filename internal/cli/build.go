package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/petroprotsakh/go-provider-mirror/internal/builder"
	"github.com/petroprotsakh/go-provider-mirror/internal/logging"
)

type buildOptions struct {
	manifestPath string
	outputDir    string
	cacheDir     string
	noCache      bool
	concurrency  int
	retries      int
	maxBackoff   int
}

func newBuildCommand() *cobra.Command {
	opts := &buildOptions{}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build a provider mirror from a manifest",
		Long: `Build a provider mirror by resolving versions, downloading provider 
binaries, and generating the filesystem layout.

The build is atomic: either it succeeds completely or produces no output.
Downloads are cached for efficient re-runs.`,
		Example: `  # Build a mirror from manifest
  provider-mirror build --manifest mirror.yaml --output ./mirror

  # Build with custom cache directory
  provider-mirror build --manifest mirror.yaml --output ./mirror --cache-dir /tmp/provider-cache

  # Force re-download, ignoring cache
  provider-mirror build --manifest mirror.yaml --output ./mirror --no-cache

  # Build with increased parallelism
  provider-mirror build --manifest mirror.yaml --output ./mirror --concurrency 8`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(
		&opts.manifestPath,
		"manifest",
		"m",
		"mirror.yaml",
		"Path to the manifest file",
	)
	cmd.Flags().StringVarP(
		&opts.outputDir,
		"output",
		"o",
		"./mirror",
		"Output directory for the mirror",
	)
	cmd.Flags().StringVar(
		&opts.cacheDir,
		"cache-dir",
		"",
		"Cache directory for downloads (default: system temp)",
	)
	cmd.Flags().BoolVar(
		&opts.noCache,
		"no-cache",
		false,
		"Ignore cached downloads and re-download all files",
	)
	cmd.Flags().IntVar(&opts.concurrency, "concurrency", 8, "Number of parallel downloads")
	cmd.Flags().IntVar(&opts.retries, "retries", 3, "Number of retries for failed downloads")
	cmd.Flags().IntVar(&opts.maxBackoff, "max-backoff", 60, "Maximum backoff time in seconds")

	return cmd
}

func runBuild(ctx context.Context, opts *buildOptions) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := builder.Config{
		ManifestPath: opts.manifestPath,
		OutputDir:    opts.outputDir,
		CacheDir:     opts.cacheDir,
		NoCache:      opts.noCache,
		Concurrency:  opts.concurrency,
		Retries:      opts.retries,
		MaxBackoff:   opts.maxBackoff,
	}

	b, err := builder.New(cfg)
	if err != nil {
		return err
	}

	if err := b.Build(ctx); err != nil {
		return err
	}

	log := logging.Default()
	if log.IsNormal() {
		log.Println("✓ Mirror built successfully")
	} else {
		log.Info("mirror built successfully")
	}

	return nil
}
