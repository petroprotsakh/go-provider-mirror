package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/petroprotsakh/go-provider-mirror/internal/logging"
	"github.com/petroprotsakh/go-provider-mirror/internal/version"
)

// globalOpts holds the global CLI options
type globalOpts struct {
	quiet     bool
	verbose   int // 0 = normal, 1 = verbose, 2+ = debug
	logFormat string
}

var gOpts globalOpts

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "provider-mirror",
		Short: "Build reproducible Terraform and OpenTofu provider mirrors",
		Long: `Provider Mirror Builder is a CLI utility for building reproducible 
Terraform and OpenTofu provider mirrors as static build artifacts.

It takes a declarative YAML manifest describing required providers and 
generates a filesystem mirror consumable by both Terraform and OpenTofu.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initLogging()
		},
	}

	// Global flags
	rootCmd.PersistentFlags().BoolVarP(
		&gOpts.quiet, "quiet", "q", false,
		"Suppress all output except errors",
	)
	rootCmd.PersistentFlags().CountVarP(
		&gOpts.verbose, "verbose", "v",
		"Increase verbosity (-v for verbose, -vv for debug)",
	)
	rootCmd.PersistentFlags().StringVar(
		&gOpts.logFormat, "log-format", "text",
		"Log output format: text or json",
	)

	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newBuildCommand())
	rootCmd.AddCommand(newVerifyCommand())
	rootCmd.AddCommand(newPlanCommand())

	return rootCmd
}

func initLogging() error {
	if gOpts.quiet && gOpts.verbose > 0 {
		return errors.New("--quiet and --verbose are mutually exclusive")
	}

	// Validate format
	var format logging.Format
	switch gOpts.logFormat {
	case "text":
		format = logging.FormatText
	case "json":
		format = logging.FormatJSON
	default:
		return fmt.Errorf("invalid log format %q: must be 'text' or 'json'", gOpts.logFormat)
	}

	// Determine level
	var level logging.Level
	if gOpts.quiet {
		level = logging.LevelQuiet
	} else if gOpts.verbose >= 2 {
		level = logging.LevelDebug
	} else if gOpts.verbose == 1 {
		level = logging.LevelVerbose
	} else {
		level = logging.LevelNormal
	}

	logging.Init(
		logging.Config{
			Level:  level,
			Format: format,
			Output: os.Stderr,
		},
	)

	return nil
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("provider-mirror %s\n", version.Version)
			fmt.Printf("  commit:       %s\n", version.Commit)
			fmt.Printf("  built:        %s\n", version.BuildTime)
		},
	}
}
