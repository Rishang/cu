package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

func newTaskCommand() *cobra.Command {
	var (
		taskfile  string
		directory string
	)

	cmd := &cobra.Command{
		Use:   "task [flags] [-- task-args...]",
		Short: "Run tasks from your personal Taskfile",
		Long: `Run tasks from a Taskfile, defaulting to ~/.config/cu/Taskfile.yml.

This execs the task binary directly so interactive tasks keep full TTY
behavior; install it from https://taskfile.dev.

  # ~/.config/cu/Taskfile.yml
  version: '3'
  tasks:
    default:
      desc: Print a greeting message
      cmds:
        - echo "Hello, World!"
      silent: true

Example:
  cu task default`,
		// Task names and their flags belong to task, not to cu.
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		RunE: func(_ *cobra.Command, args []string) error {
			binary, err := exec.LookPath("task")
			if err != nil {
				return fmt.Errorf("task not found in PATH — install it from https://taskfile.dev")
			}

			argv := append([]string{"task", "-t", taskfile, "-d", directory}, args...)
			return syscall.Exec(binary, argv, os.Environ())
		},
	}

	defaultTaskfile := filepath.Join(configHome(), "Taskfile.yml")
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	// Stop parsing at the first positional: everything from the task name on
	// belongs to task, so `cu task deploy --force` keeps --force.
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringVarP(&taskfile, "taskfile", "t", defaultTaskfile, "Taskfile to run.")
	cmd.Flags().StringVarP(&directory, "directory", "d", cwd, "Directory to run tasks in.")
	return cmd
}

// configHome is where cu keeps its own config: ~/.config/cu.
func configHome() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "cu")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config/cu"
	}
	return filepath.Join(home, ".config", "cu")
}
