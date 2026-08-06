package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Rishang/cloudutil/internal/ui"
)

// Helpers shared by more than one command. Anything used by a single command
// belongs in that command's file instead.

// configHome is where cu keeps its own config: ~/.config/cu.
func configHome() string {
	// UserConfigDir already falls back to $HOME/.config, so it only fails when
	// neither $XDG_CONFIG_HOME nor $HOME is set.
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".config/cu"
	}
	return filepath.Join(dir, "cu")
}

// decodeSecretValue nests JSON secrets in the output and passes anything else
// through as a plain string.
func decodeSecretValue(value string) any {
	var parsed any
	if json.Unmarshal([]byte(value), &parsed) == nil {
		if _, isObject := parsed.(map[string]any); isObject {
			return parsed
		}
	}
	return value
}

// captureFromEditor opens $EDITOR on an empty temp file and returns what was
// written. name is the temp file's basename, which is what gives the editor a
// syntax hint.
func captureFromEditor(name string) (string, error) {
	dir, err := os.MkdirTemp("", "cu-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return "", err
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
		if _, err := exec.LookPath(editor); err != nil {
			editor = "nano"
		}
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	runErr := cmd.Run()
	// The editor can hand back control mid-line; put the cursor back at
	// column 0 before anything else prints, regardless of why.
	ui.ResetCursor()
	if runErr != nil {
		return "", fmt.Errorf("editor %q failed: %w", editor, runErr)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// openBrowser opens a URL with the platform's default handler.
func openBrowser(url string) error {
	openers := []string{"xdg-open", "open", "wslview"}
	for _, opener := range openers {
		if binary, err := exec.LookPath(opener); err == nil {
			ui.Info("Opening URL in your default browser (%s)...", opener)
			return exec.Command(binary, url).Start()
		}
	}
	return fmt.Errorf("no browser opener found (tried %s)", strings.Join(openers, ", "))
}
