package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	// only killing the process. stop() runs as soon as the first signal lands,
	// restoring the default handler, so a second Ctrl-C still aborts a command
	// that ignores its context; deferring it alone would keep catching signals
	// until main returned.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

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
	// A child cu handed the terminal to (task, session-manager-plugin on
	// Windows, where there is no exec) has printed its own failure; pass its
	// code through rather than adding a vaguer one of ours.
	var child *exec.ExitError
	if errors.As(err, &child) {
		os.Exit(child.ExitCode())
	}

	ui.Error("%v", err)
	os.Exit(1)
}
