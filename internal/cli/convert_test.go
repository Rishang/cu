package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Rishang/cloudutil/internal/ui"
)

// captureOut redirects ui.Out for one test.
func captureOut(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := ui.Out
	ui.Out = &buf
	t.Cleanup(func() { ui.Out = prev })
	return &buf
}

func TestYAMLToJSONKeepsEveryDocument(t *testing.T) {
	buf := captureOut(t)

	// The k8s manifest shape: dropping anything after the first '---' would be
	// silent data loss.
	if err := yamlToJSON([]byte("kind: A\nn: 1\n---\nkind: B\nn: 2\n")); err != nil {
		t.Fatalf("yamlToJSON: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d line(s), want 2: %q", len(lines), buf.String())
	}
	for i, want := range []string{`"kind": "A"`, `"kind": "B"`} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want it to contain %s", i, lines[i], want)
		}
	}
}

// Key order is why the converter writes bytes instead of going through
// ui.PrintJSON: decoding into a map would sort these alphabetically.
func TestYAMLToJSONKeepsKeyOrder(t *testing.T) {
	buf := captureOut(t)

	if err := yamlToJSON([]byte("zeta: 1\nalpha: 2\nmiddle: 3\n")); err != nil {
		t.Fatalf("yamlToJSON: %v", err)
	}

	got := buf.String()
	if z, a := strings.Index(got, "zeta"), strings.Index(got, "alpha"); z > a {
		t.Errorf("keys were sorted, want source order: %q", got)
	}
}

func TestYAMLToJSONRejectsInvalidInput(t *testing.T) {
	captureOut(t)

	err := yamlToJSON([]byte("key: [unclosed\n"))
	if err == nil {
		t.Fatal("got nil error for malformed YAML, want one")
	}
	if !strings.Contains(err.Error(), "invalid YAML") {
		t.Errorf("error = %q, want it to mention invalid YAML", err)
	}
}
