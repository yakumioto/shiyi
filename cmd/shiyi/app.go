package main

import (
	"context"
	"fmt"
	"io"
)

const version = "shiyi 1.0.0"

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// run dispatches service management only. Help, version and password hashing
// do not read serve configuration or contact the service.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "shiyi: %v\n\n%s", err, usageText)
		return exitUsage
	}
	switch opts.Mode {
	case ModeHelp:
		fmt.Fprint(stdout, usageText)
		return exitOK
	case ModeVersion:
		fmt.Fprintln(stdout, version)
		return exitOK
	case ModeServe:
		return runServe(ctx, opts, stdout, stderr)
	case ModeHashPassword:
		return runHashPassword(ctx, stdin, stdout, stderr)
	default:
		return exitUsage
	}
}
