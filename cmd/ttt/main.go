package main

import (
	"fmt"
	"os"

	"ttt/internal/cli"
)

// version is stamped by the Makefile via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
