package main

import (
	"io"
	"os"

	"braid/internal/command"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return command.NewApp().Run(args, stdout, stderr)
}
