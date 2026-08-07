package pick

import (
	"reflect"
	"testing"

	fzf "github.com/junegunn/fzf/src"
)

var services = []string{"prod/api", "dev/api", "dev/worker", "stage/api"}

// fzf runs in-process. --filter is its non-interactive mode, so this exercises
// the real embedding — channels in, selections out — without needing a TTY.
func TestRunFZFFilterMode(t *testing.T) {
	lines, err := runFZF(services, []string{"--filter=dev", "--no-sort"})
	if err != nil {
		t.Fatalf("runFZF: %v", err)
	}
	want := []string{"dev/api", "dev/worker"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("selected %v, want %v", lines, want)
	}
}

// No match is a normal outcome, not an error: fzf exits non-zero and the
// selection simply comes back empty.
func TestRunFZFNoMatch(t *testing.T) {
	lines, err := runFZF(services, []string{"--filter=nonexistent-xyz"})
	if err != nil {
		t.Fatalf("runFZF: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("selected %v, want nothing", lines)
	}
}

func TestRunFZFExactMatch(t *testing.T) {
	lines, err := runFZF(services, []string{"--exact", "--filter=stage/api"})
	if err != nil {
		t.Fatalf("runFZF: %v", err)
	}
	if !reflect.DeepEqual(lines, []string{"stage/api"}) {
		t.Fatalf("selected %v, want [stage/api]", lines)
	}
}

func TestRunFZFRejectsBadOptions(t *testing.T) {
	if _, err := runFZF(services, []string{"--definitely-not-a-flag"}); err == nil {
		t.Fatal("expected an error for an unknown fzf option")
	}
}

func TestFZFArgs(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{"bare", Options{}, []string{"--exact", "--ansi"}},
		{"multi and prompt", Options{Multi: true, Prompt: "pod> "},
			[]string{"--exact", "--ansi", "--multi", "--prompt=pod> "}},
		{"preview brings its own window and toggle", Options{Preview: "echo {}"},
			[]string{"--exact", "--ansi", "--preview=echo {}",
				"--preview-window=right,50%,wrap", "--preview-wrap-sign=  ",
				"--bind=ctrl-o:toggle-preview",
				"--bind=ctrl-]:change-preview-window(right,75%|right,25%|right,50%)"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fzfArgs(tc.opts); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("fzfArgs() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Whatever fzfArgs produces has to be something fzf will actually accept.
func TestFZFArgsAreValid(t *testing.T) {
	args := fzfArgs(Options{Multi: true, Prompt: "pod> ", Preview: "echo {}"})
	if _, err := fzf.ParseOptions(true, append(args, "--filter=dev")); err != nil {
		t.Fatalf("fzf rejected %q: %v", args, err)
	}
}

// Selecting from an empty list must not launch fzf at all.
func TestSelectEmptyItems(t *testing.T) {
	got, err := Select(nil, func(s string) string { return s }, Options{})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}
