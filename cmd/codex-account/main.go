package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/webville-dev/codex-account/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, nil))
}
