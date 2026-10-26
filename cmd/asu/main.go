package main

import (
	"os"

	"github.com/gzzchh/asu-go/internal/cli"
)

func main() {
	if err := cli.NewCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
