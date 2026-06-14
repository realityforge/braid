package main

import (
	"io"
	"os"

	"braid/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return cli.New().Run(args, stdout, stderr)
}
