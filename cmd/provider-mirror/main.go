package main

import (
	"fmt"
	"os"

	"github.com/petroprotsakh/go-provider-mirror/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
