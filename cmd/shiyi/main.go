package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// main is the entry point of the shiyi command.
//
// The signal context is cancelled on SIGINT and SIGTERM so that the service
// can shut down gracefully.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, os.Args[1:], newSender, os.Stdin, os.Stdout, os.Stderr))
}
