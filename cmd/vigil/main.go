package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ValidatorStack/solana-vigil/internal/agent"
	"github.com/ValidatorStack/solana-vigil/internal/cli"
)

func main() {
	agent.Ensure()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
