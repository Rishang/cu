package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rishang/cloudutil/internal/ui"
)

func asset(name string) string {
	return filepath.Join("..", "..", "tests", "assets", name)
}

// captureUI redirects ui.Out/ui.Err to fresh buffers, disables color, and
// restores both once the test ends.
func captureUI(t *testing.T) (outBuf, errBuf *bytes.Buffer) {
	t.Helper()

	outBuf, errBuf = &bytes.Buffer{}, &bytes.Buffer{}
	prevOut, prevErr, prevColor := ui.Out, ui.Err, ui.ColorEnabled()
	ui.Out, ui.Err = outBuf, errBuf
	ui.SetColor(false)
	t.Cleanup(func() {
		ui.Out, ui.Err = prevOut, prevErr
		ui.SetColor(prevColor)
	})
	return outBuf, errBuf
}

// runCu executes the command tree with args and returns stdout, stderr and the
// resulting exit code (0 when the command succeeded).
func runCu(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	outBuf, errBuf := captureUI(t)

	root := NewRootCommand()
	root.SetArgs(args)
	root.SetOut(outBuf)
	root.SetErr(errBuf)

	err := root.Execute()
	switch {
	case err == nil:
		code = 0
	default:
		var exit ExitCodeError
		if errors.As(err, &exit) {
			code = exit.Code
		} else {
			code = 1
			errBuf.WriteString(err.Error())
		}
	}
	return outBuf.String(), errBuf.String(), code
}

// N-way comparison expands to every 2-combination, in order.
func TestIndexPairs(t *testing.T) {
	cases := []struct {
		n    int
		want [][2]int
	}{
		{0, nil},
		{1, nil},
		{2, [][2]int{{0, 1}}},
		{3, [][2]int{{0, 1}, {0, 2}, {1, 2}}},
		{4, [][2]int{{0, 1}, {0, 2}, {0, 3}, {1, 2}, {1, 3}, {2, 3}}},
	}

	for _, tc := range cases {
		got := indexPairs(tc.n)
		if len(got) != len(tc.want) {
			t.Fatalf("indexPairs(%d) returned %d pairs, want %d", tc.n, len(got), len(tc.want))
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("indexPairs(%d) = %v, want %v", tc.n, got, tc.want)
			}
		}
	}
}

func TestDiffExitCodes(t *testing.T) {
	t.Run("differences exit 1", func(t *testing.T) {
		_, _, code := runCu(t, "diff", "-f", asset("config-a.json"), "-f", asset("config-b.yaml"))
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("identical files exit 0", func(t *testing.T) {
		_, _, code := runCu(t, "diff", "-f", asset("app-dev.yaml"), "-f", asset("app-dev.yaml"))
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	})
}

func TestDiffArgumentErrors(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"both -f and --config",
			[]string{"diff", "-f", asset("app-dev.yaml"), "--config", asset("cu_diff.yml")},
			"not both"},
		{"single -f",
			[]string{"diff", "-f", asset("app-dev.yaml")},
			"At least 2"},
		{"missing file",
			[]string{"diff", "-f", asset("app-dev.yaml"), "-f", "/nonexistent.yaml"},
			"File not found"},
		{"bad format",
			[]string{"diff", "-o", "xml", "-f", asset("app-dev.yaml"), "-f", asset("app-prod.yaml")},
			"Invalid format"},
		{"bad query",
			[]string{"diff", "-q", "[?kind==", "-f", asset("app-dev.yaml"), "-f", asset("app-prod.yaml")},
			"invalid JMESPath"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCu(t, tc.args...)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Fatalf("stderr does not contain %q:\n%s", tc.wantErr, stderr)
			}
		})
	}
}

func TestDiffPrintSchema(t *testing.T) {
	stdout, _, code := runCu(t, "diff", "--print-schema")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{"diffs", "global_ignore_patterns", "format"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("schema is missing %q", want)
		}
	}
}

// Config files resolve their file paths relative to the config's own directory.
func TestDiffConfigMode(t *testing.T) {
	stdout, stderr, code := runCu(t, "diff", "--config", asset("cu_diff.yml"), "-o", "json")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (the fixtures differ)", code)
	}
	if strings.Contains(stderr, "File not found") {
		t.Fatalf("relative paths failed to resolve:\n%s", stderr)
	}
	// Three files means three pairs, so three JSON documents.
	if got := strings.Count(stdout, `"diffs"`); got != 3 {
		t.Errorf("got %d rendered pairs, want 3", got)
	}
}

func TestDiffJSONGoesToStdoutOnly(t *testing.T) {
	stdout, stderr, _ := runCu(t, "diff", "-o", "json",
		"-f", asset("app-dev.yaml"), "-f", asset("app-prod.yaml"))

	if !strings.Contains(stdout, `"diffs"`) {
		t.Errorf("stdout has no JSON payload:\n%s", stdout)
	}
	if strings.Contains(stderr, `"diffs"`) {
		t.Errorf("JSON leaked into stderr:\n%s", stderr)
	}
}

func TestDiffIgnoreFlags(t *testing.T) {
	t.Run("--ignore-key suppresses a section", func(t *testing.T) {
		stdout, _, _ := runCu(t, "diff", "-o", "json", "--ignore-key", "database",
			"-f", asset("app-dev.yaml"), "-f", asset("app-prod.yaml"))
		if strings.Contains(stdout, "database.") {
			t.Errorf("database paths survived --ignore-key:\n%s", stdout)
		}
	})

	t.Run("--ignore-pattern suppresses env-only changes", func(t *testing.T) {
		stdout, _, _ := runCu(t, "diff", "-o", "json",
			"--ignore-pattern", "dev,prod",
			"-f", asset("app-dev.yaml"), "-f", asset("app-prod.yaml"))
		if strings.Contains(stdout, `"app.host"`) {
			t.Errorf("app.host survived --ignore-pattern:\n%s", stdout)
		}
		if !strings.Contains(stdout, `"app.replicas"`) {
			t.Errorf("app.replicas should have been kept:\n%s", stdout)
		}
	})
}

// The command surface is frozen: these flags and subcommands must keep working.
func TestCommandSurface(t *testing.T) {
	groups := []string{"aws", "az", "os", "k", "pwpush", "diff", "task"}

	root := NewRootCommand()
	registered := map[string]bool{}
	for _, cmd := range root.Commands() {
		registered[cmd.Name()] = true
	}
	for _, name := range groups {
		if !registered[name] {
			t.Errorf("missing command group %q", name)
		}
	}

	subcommands := map[string][]string{
		"aws":    {"login", "ssm-parameters", "ec2-ssm", "secrets", "decode-message"},
		"az":     {"secrets"},
		"k":      {"secrets", "configmaps", "ctx"},
		"os":     {"history"},
		"pwpush": {"config", "list-active", "send", "pwgen"},
	}
	for _, cmd := range root.Commands() {
		want, tracked := subcommands[cmd.Name()]
		if !tracked {
			continue
		}
		have := map[string]bool{}
		for _, sub := range cmd.Commands() {
			have[sub.Name()] = true
		}
		for _, name := range want {
			if !have[name] {
				t.Errorf("cu %s is missing subcommand %q", cmd.Name(), name)
			}
		}
	}
}

// Shell completion is why cobra was chosen; make sure every shell emits a script.
func TestCompletionsAreGenerated(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			stdout, stderr, code := runCu(t, "completion", shell)
			if code != 0 {
				t.Fatalf("exit code = %d, stderr: %s", code, stderr)
			}
			if len(stdout) < 100 {
				t.Fatalf("completion script looks empty (%d bytes)", len(stdout))
			}
			if !strings.Contains(stdout, "cu") {
				t.Errorf("completion script does not mention the cu binary")
			}
		})
	}
}
