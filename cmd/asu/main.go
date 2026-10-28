package main

import (
	"os"

	"github.com/chisaato/asu-go/internal/cli"
)

func main() {
	if err := cli.NewCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
