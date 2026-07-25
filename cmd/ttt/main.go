package main

import (
	"os"

	"ttt/internal/cli"
)

// version is stamped by the Makefile via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// Execute prints the error itself (text or JSON, per --json); main only
	// translates it into the exit code.
	if err := cli.Execute(version); err != nil {
		os.Exit(1)
	}
}
