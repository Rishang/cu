package main

import (
	"errors"
	"fmt"
	"os"

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

	err := root.Execute()
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
