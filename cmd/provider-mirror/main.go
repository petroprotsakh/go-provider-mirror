package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/petroprotsakh/go-provider-mirror/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		if errors.Is(err, context.Canceled) {
			_, _ = fmt.Fprintln(os.Stderr, "\nInterrupted")
			os.Exit(130) // SIGINT
		}
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
