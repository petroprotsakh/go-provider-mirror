package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/petroprotsakh/go-provider-mirror/internal/logging"
	"github.com/petroprotsakh/go-provider-mirror/internal/planner"
)

type planOptions struct {
	manifestPath string
}

func newPlanCommand() *cobra.Command {
	opts := &planOptions{}

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what would be downloaded without building the mirror (dry-run)",
		Long: `Plan resolves provider versions and shows what would be downloaded
without actually downloading anything.

Use this to preview the build before committing to it.`,
		Example: `  # Preview what will be downloaded
  provider-mirror plan --manifest mirror.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlan(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(
		&opts.manifestPath,
		"manifest",
		"m",
		"mirror.yaml",
		"Path to the manifest file",
	)

	return cmd
}

func runPlan(ctx context.Context, opts *planOptions) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	p, err := planner.New(opts.manifestPath)
	if err != nil {
		return err
	}

	plan, err := p.Plan(ctx)
	if err != nil {
		return err
	}

	log := logging.Default()
	if log.IsNormal() {
		log.Print(
			"Plan: %d providers, %d versions, %d downloads\n\n",
			len(plan.Providers), plan.TotalVersions, plan.TotalDownloads,
		)

		for _, prov := range plan.Providers {
			log.Print("  %s\n", prov.Source)
			for _, v := range prov.Versions {
				log.Print("    %s (%d platforms)\n", v.Version, len(v.Platforms))
			}
		}
	} else {
		logging.Info(
			"plan complete",
			"providers", len(plan.Providers),
			"versions", plan.TotalVersions,
			"downloads", plan.TotalDownloads,
		)

		for _, prov := range plan.Providers {
			for _, v := range prov.Versions {
				logging.Verbose(
					"would download",
					"provider", prov.Source,
					"version", v.Version,
					"platforms", v.Platforms,
				)
			}
		}
	}

	return nil
}
