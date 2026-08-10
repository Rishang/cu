package diff

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Rishang/cloudutil/internal/ui"
)

// capture redirects ui output into buffers and disables color for the test.
func capture(t *testing.T) (stdout, stderr *bytes.Buffer) {
	t.Helper()
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}

	prevOut, prevErr, prevColor := ui.Out, ui.Err, ui.ColorEnabled()
	ui.Out, ui.Err = stdout, stderr
	ui.SetColor(false)
	t.Cleanup(func() {
		ui.Out, ui.Err = prevOut, prevErr
		ui.SetColor(prevColor)
	})
	return stdout, stderr
}

func TestFormatValue(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"null", nil, "null"},
		{"string is quoted", "hello", "'hello'"},
		{"empty string", "", "''"},
		{"int", int64(42), "42"},
		{"float", 2.5, "2.5"},
		{"bool", true, "true"},
		{"map as compact json", map[string]any{"a": int64(1)}, `{"a":1}`},
		{"list as compact json", []any{int64(1), "b"}, `[1,"b"]`},
		{"html is not escaped", map[string]any{"q": "a&b"}, `{"q":"a&b"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatValue(tc.value); got != tc.want {
				t.Fatalf("formatValue(%#v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestTypeName(t *testing.T) {
	cases := map[string]any{
		"null":   nil,
		"bool":   true,
		"int":    int64(1),
		"float":  1.5,
		"string": "s",
		"map":    map[string]any{},
		"list":   []any{},
	}
	for want, value := range cases {
		if got := typeName(value); got != want {
			t.Errorf("typeName(%#v) = %q, want %q", value, got, want)
		}
	}
}

func TestRelativeKey(t *testing.T) {
	cases := []struct {
		path, section, want string
	}{
		{"spec", "spec", ""},
		{"spec.replicas", "spec", "replicas"},
		{"spec.template.image", "spec", "template.image"},
		{"users[0].name", "users", "[0].name"},
		{"metadata.name", "spec", "metadata.name"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := relativeKey(tc.path, tc.section); got != tc.want {
				t.Fatalf("relativeKey(%q, %q) = %q, want %q",
					tc.path, tc.section, got, tc.want)
			}
		})
	}
}

func TestExpandFlattensToLeaves(t *testing.T) {
	pairs := expand("db", map[string]any{
		"host":  "localhost",
		"ports": []any{int64(1), int64(2)},
		"creds": map[string]any{"user": "admin"},
	})

	got := map[string]any{}
	for _, kv := range pairs {
		got[kv.key] = kv.value
	}
	want := map[string]any{
		"db.creds.user": "admin",
		"db.host":       "localhost",
		"db.ports[0]":   int64(1),
		"db.ports[1]":   int64(2),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}
}

func TestExpandEmptyContainersStayWhole(t *testing.T) {
	for _, value := range []any{map[string]any{}, []any{}} {
		pairs := expand("k", value)
		if len(pairs) != 1 || pairs[0].key != "k" {
			t.Fatalf("expand(%#v) = %+v, want a single k entry", value, pairs)
		}
	}
}

func TestExpandNoPrefix(t *testing.T) {
	pairs := expand("", map[string]any{"a": int64(1)})
	if len(pairs) != 1 || pairs[0].key != "a" {
		t.Fatalf("got %+v, want key a", pairs)
	}
}

func TestWordDiffHighlightsOnlyTheDifference(t *testing.T) {
	oldCell, newCell := wordDiff("t2.micro", "t3.small")

	if joinSegments(oldCell) != "t2.micro" {
		t.Errorf("old cell text = %q", joinSegments(oldCell))
	}
	if joinSegments(newCell) != "t3.small" {
		t.Errorf("new cell text = %q", joinSegments(newCell))
	}
	// "t" is shared, so the first segment is the common prefix.
	if oldCell[0].Text != "t" {
		t.Errorf("common prefix = %q, want t", oldCell[0].Text)
	}
}

func TestWordDiffIdenticalValues(t *testing.T) {
	oldCell, newCell := wordDiff("same", "same")
	if len(oldCell) != 1 || len(newCell) != 1 {
		t.Fatalf("identical values produced %d/%d segments, want 1/1", len(oldCell), len(newCell))
	}
}

func TestWordDiffEmptySide(t *testing.T) {
	oldCell, newCell := wordDiff("", "added")
	if joinSegments(oldCell) != "" || joinSegments(newCell) != "added" {
		t.Fatalf("got %q / %q", joinSegments(oldCell), joinSegments(newCell))
	}
}

func joinSegments(cell ui.Cell) string {
	var b strings.Builder
	for _, seg := range cell {
		b.WriteString(seg.Text)
	}
	return b.String()
}

func TestRenderJSONShape(t *testing.T) {
	stdout, stderr := capture(t)

	entries := []Entry{
		changed([]any{"spec", "replicas"}, int64(1), int64(3)),
		entry([]any{"spec", "extra"}, KindAdded, nil, "new"),
	}
	Render(entries, RenderOptions{
		Format: FormatJSON, FileA: "a.yaml", FileB: "b.yaml", BranchA: "main",
	})

	if stderr.Len() != 0 {
		t.Errorf("JSON format wrote to stderr: %q", stderr.String())
	}

	var out struct {
		Files struct {
			Old struct{ File, Branch string }
			New struct{ File, Branch string }
		}
		Diffs []struct {
			Path, Kind string
			Old, New   any
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}

	if out.Files.Old.File != "a.yaml" || out.Files.Old.Branch != "main" {
		t.Errorf("files.old = %+v", out.Files.Old)
	}
	if out.Files.New.Branch != "" {
		t.Errorf("absent branch should be omitted, got %q", out.Files.New.Branch)
	}
	if len(out.Diffs) != 2 {
		t.Fatalf("got %d diffs, want 2", len(out.Diffs))
	}
	// Entries are sorted by path.
	if out.Diffs[0].Path != "spec.extra" || out.Diffs[1].Path != "spec.replicas" {
		t.Errorf("diffs are not sorted by path: %+v", out.Diffs)
	}
}

func TestRenderJSONEmptyDiffsIsArray(t *testing.T) {
	stdout, _ := capture(t)
	Render(nil, RenderOptions{Format: FormatJSON, FileA: "a", FileB: "b"})

	if !strings.Contains(stdout.String(), `"diffs": []`) {
		t.Fatalf("want an empty array, got %s", stdout.String())
	}
}

func TestRenderTable(t *testing.T) {
	_, stderr := capture(t)

	Render([]Entry{
		changed([]any{"app", "version"}, "1.0.0", "2.0.0"),
		entry([]any{"app", "ssl"}, KindAdded, nil, true),
		entry([]any{"app", "old"}, KindRemoved, "gone", nil),
	}, RenderOptions{Format: FormatTable, FileA: "a.yaml", FileB: "b.yaml"})

	out := stderr.String()
	for _, want := range []string{
		"Path", "a.yaml", "b.yaml",
		"app.version", "'1.0.0'", "'2.0.0'",
		"app.ssl", "app.old",
		"added", "removed", "changed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output is missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTableNoDifferences(t *testing.T) {
	_, stderr := capture(t)
	Render(nil, RenderOptions{Format: FormatTable, FileA: "a", FileB: "b"})

	if !strings.Contains(stderr.String(), "No differences") {
		t.Fatalf("got %q", stderr.String())
	}
}

func TestRenderUnified(t *testing.T) {
	_, stderr := capture(t)

	Render([]Entry{
		changed([]any{"spec", "replicas"}, int64(1), int64(3)),
		entry([]any{"spec", "image"}, KindAdded, nil, "nginx"),
		entry([]any{"meta", "uid"}, KindRemoved, "abc", nil),
		entry([]any{"spec", "port"}, KindTypeChanged, "80", int64(8080)),
	}, RenderOptions{
		Format: FormatUnified, FileA: "a.yaml", FileB: "b.yaml",
		BranchA: "main", BranchB: "feature",
	})

	// No file header here: cli.printPairHeader already names both files and
	// their branches directly above this output.
	out := stderr.String()
	for _, want := range []string{
		"@@ spec @@", "@@ meta @@",
		"replicas:", "1 → 3",
		"(string → int)", // the type-change note
		"3 changes",      // spec section groups three entries
	} {
		if !strings.Contains(out, want) {
			t.Errorf("unified output is missing %q:\n%s", want, out)
		}
	}
}

func TestRenderIgnoredSection(t *testing.T) {
	_, stderr := capture(t)

	Render([]Entry{changed([]any{"app", "replicas"}, int64(1), int64(2))},
		RenderOptions{
			Format: FormatTable, FileA: "a", FileB: "b",
			Ignored: []Entry{
				changed([]any{"app", "host"}, "dev-api", "prod-api"),
				changed([]any{"db", "host"}, "dev-db", "prod-db"),
			},
		})

	out := stderr.String()
	if !strings.Contains(out, "Ignored (2)") {
		t.Errorf("missing the ignored count:\n%s", out)
	}
	for _, want := range []string{"app.host", "db.host"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing ignored path %q:\n%s", want, out)
		}
	}
}
