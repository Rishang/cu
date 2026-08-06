package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Rishang/cloudutil/internal/cli"
	"github.com/Rishang/cloudutil/internal/ui"
)

// Stamped by goreleaser at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := cli.NewRootCommand()
	root.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)

	// Ctrl-C cancels in-flight work (a Vault walk, an SSM call) instead of
	// only killing the process; stop() restores the default handler so a
	// second Ctrl-C still aborts if a command ignores its context.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := root.ExecuteContext(ctx)
	if err == nil {
		return
	}

	// A command asking for a specific exit code has already printed whatever
	// the user needs to see.
	var exit cli.ExitCodeError
	if errors.As(err, &exit) {
		os.Exit(exit.Code)
	}

	ui.Error("%v", err)
	os.Exit(1)
}
