package cli

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rishang/cloudutil/internal/pick"
	"github.com/Rishang/cloudutil/internal/ui"
)

func newOSCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "os",
		Short: "OS-related utilities",
	}
	cmd.AddCommand(newHistoryCommand())
	return cmd
}

// zshTimestamp matches the ": <epoch>:<duration>;" prefix zsh writes in
// extended history mode.
var zshTimestamp = regexp.MustCompile(`^: [0-9]*:[0-9]*;`)

func newHistoryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "Search shell history and print the selection",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			shell := os.Getenv("SHELL")

			var (
				file  string
				multi bool
			)
			switch {
			case strings.Contains(shell, "zsh"):
				file, multi = ".zsh_history", true
			case strings.Contains(shell, "bash"):
				file, multi = ".bash_history", false
			default:
				ui.Error("Unsupported shell: %q. Supported: zsh, bash.", shell)
				return exitWith(1)
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			entries, err := readHistory(filepath.Join(home, file))
			if err != nil {
				return err
			}
			selected, err := pickStrings(entries, "history command", pick.Options{
				Multi:  multi,
				Prompt: "history> ",
			})
			if err != nil {
				return err
			}
			for _, line := range selected {
				fmt.Fprintln(ui.Out, line)
			}
			return nil
		},
	}
}

// readHistory returns unique, sorted history entries.
func readHistory(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("history file not found: %s", path)
		}
		return nil, err
	}
	defer f.Close()

	unique := map[string]struct{}{}
	scanner := bufio.NewScanner(f)
	// History files can hold very long lines (pasted commands).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(zshTimestamp.ReplaceAllString(scanner.Text(), ""))
		if line != "" {
			unique[line] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return slices.Sorted(maps.Keys(unique)), nil
}
