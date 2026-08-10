package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	yaml "github.com/goccy/go-yaml"

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

// cuConfig is cu's own settings, all persisted in the one file at
// ~/.config/cu/config.yml: Password Pusher auth under "psst", secret backend
// profiles under "vault".
type cuConfig struct {
	Psst  pwpushConfig     `yaml:"psst"`
	Vault []secretProvider `yaml:"vault"`
}

func configPath() string { return filepath.Join(configHome(), "config.yml") }

// loadConfig reads config.yml, returning a zero-value config if it doesn't
// exist yet — callers decide whether an empty section is an error.
func loadConfig() (*cuConfig, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &cuConfig{}, nil
		}
		return nil, err
	}
	// The file holds tokens and client secrets in plaintext, so say so when
	// anyone else on the box can read it.
	if info, err := os.Stat(configPath()); err == nil && info.Mode().Perm()&0o077 != 0 {
		ui.Warn("%s is readable by other users — chmod 600 it.", configPath())
	}
	cfg := &cuConfig{}
	if err := yaml.Unmarshal(expandConfigEnv(data), cfg); err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", configPath(), err)
	}
	return cfg, nil
}

// configEnvVar matches ${VAR} references in config.yml. Deliberately narrower
// than shell expansion (no bare $VAR, no $$) so a token that happens to
// contain a literal "$" is not silently mangled.
var configEnvVar = regexp.MustCompile(`\$\{(\w+)\}`)

// expandConfigEnv substitutes ${VAR} with the environment variable's value,
// so config.yml can hold a reference instead of the secret itself. An unset
// variable expands to empty, like a shell would.
func expandConfigEnv(data []byte) []byte {
	return configEnvVar.ReplaceAllFunc(data, func(match []byte) []byte {
		name := match[2 : len(match)-1]
		return []byte(os.Getenv(string(name)))
	})
}

// saveConfig writes config.yml. Kept owner-readable only, since it holds
// tokens and client secrets in plaintext.
func saveConfig(cfg *cuConfig) error {
	if err := os.MkdirAll(configHome(), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath(), data, 0o600); err != nil {
		return err
	}
	// WriteFile's mode applies only on creation; enforce it too when a
	// pre-existing config file had broader permissions.
	return os.Chmod(configPath(), 0o600)
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

	// vi rather than a LookPath dance over vim/nano: POSIX mandates it, and
	// probing early only moves the same "no editor" failure a line sooner.
	editor := cmp.Or(os.Getenv("EDITOR"), "vi")
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
