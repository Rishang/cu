package pick

import (
	"reflect"
	"testing"
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

type instance struct {
	id   string
	name string
}

// Select maps fzf's chosen lines back to the original domain objects.
func TestSelectMapsLabelsBackToItems(t *testing.T) {
	items := []instance{
		{"i-111", "web"},
		{"i-222", "worker"},
	}
	label := func(i instance) string { return i.id + " | " + i.name }

	index := map[string]int{}
	for i, item := range items {
		index[label(item)] = i
	}
	if got := index["i-222 | worker"]; got != 1 {
		t.Fatalf("label mapping resolved to %d, want 1", got)
	}
}
