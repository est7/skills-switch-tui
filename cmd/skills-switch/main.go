package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/est7/skills-switch-tui/internal/cli"
)

var version = "dev"

func main() {
	// Ctrl-C or SIGTERM cancels the command context so long-running git
	// operations (source add/update/remove) abort instead of running on
	// after the user gave up.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.NewRootCommand(version).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
