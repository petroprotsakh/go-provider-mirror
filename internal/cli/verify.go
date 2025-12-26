package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/petroprotsakh/go-provider-mirror/internal/logging"
	"github.com/petroprotsakh/go-provider-mirror/internal/verifier"
)

type verifyOptions struct {
	mirrorDir string
}

func newVerifyCommand() *cobra.Command {
	opts := &verifyOptions{}

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify a provider mirror's integrity",
		Long: `Verify that a provider mirror is complete and all checksums match.

This command validates:
- All expected provider files are present
- All checksums match the recorded values
- The mirror structure is valid for both Terraform and OpenTofu`,
		Example: `  # Verify a mirror
  provider-mirror verify --mirror ./mirror`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.mirrorDir, "mirror", "./mirror", "Path to the mirror directory")

	return cmd
}

func runVerify(ctx context.Context, opts *verifyOptions) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	v := verifier.New(opts.mirrorDir)

	result, err := v.Verify(ctx)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	log := logging.Default()

	if !result.Valid {
		if log.IsNormal() {
			log.Println("✗ Mirror verification failed:")
			for _, e := range result.Errors {
				log.Print("  - %s\n", e)
			}
		} else {
			for _, e := range result.Errors {
				log.Error("verification error", "error", e)
			}
		}
		return fmt.Errorf("mirror is invalid")
	}

	if log.IsNormal() {
		log.Println("✓ Mirror verified successfully")
		log.Print("  Providers: %d\n", result.ProviderCount)
		log.Print("  Versions:  %d\n", result.VersionCount)
		log.Print("  Files:     %d\n", result.FileCount)
	} else {
		log.Info("mirror verified successfully",
			"providers", result.ProviderCount,
			"versions", result.VersionCount,
			"files", result.FileCount,
		)
	}

	return nil
}
