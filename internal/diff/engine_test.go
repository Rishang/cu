package diff

import (
	"testing"
)

func paths(entries []Entry) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		out[e.PathStr()] = true
	}
	return out
}

func TestComputeNoDifferences(t *testing.T) {
	same := map[string]any{"spec": map[string]any{"replicas": 3, "image": "nginx"}}

	cases := []struct {
		name string
		a, b any
	}{
		{"identical maps", map[string]any{"a": 1, "b": 2}, map[string]any{"a": 1, "b": 2}},
		{"identical nested", same, same},
		{"key order is irrelevant",
			map[string]any{"b": 2, "a": 1}, map[string]any{"a": 1, "b": 2}},
		{"key order inside list items is irrelevant",
			[]any{map[string]any{"a": 2, "z": 1}}, []any{map[string]any{"z": 1, "a": 2}}},
		{"quoting only: '8080' equals 8080",
			map[string]any{"port": "8080"}, map[string]any{"port": 8080}},
		{"int equals float of the same value",
			map[string]any{"v": 1}, map[string]any{"v": 1.0}},
		{"null equals null",
			map[string]any{"key": nil}, map[string]any{"key": nil}},
		{"empty maps", map[string]any{}, map[string]any{}},
		{"empty slices", []any{}, []any{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compute(tc.a, tc.b); len(got) != 0 {
				t.Fatalf("expected no differences, got %d: %+v", len(got), got)
			}
		})
	}
}

func TestComputeAddedAndRemoved(t *testing.T) {
	t.Run("added key", func(t *testing.T) {
		entries := Compute(map[string]any{"a": 1}, map[string]any{"a": 1, "b": 2})
		if len(entries) != 1 {
			t.Fatalf("want 1 entry, got %d", len(entries))
		}
		e := entries[0]
		if e.Kind != KindAdded || e.PathStr() != "b" {
			t.Fatalf("want added at b, got %s at %s", e.Kind, e.PathStr())
		}
		if e.New != int64(2) {
			t.Errorf("New = %v (%T), want int64(2)", e.New, e.New)
		}
		if e.Old != nil {
			t.Errorf("Old = %v, want nil", e.Old)
		}
	})

	t.Run("removed key", func(t *testing.T) {
		entries := Compute(map[string]any{"a": 1, "b": 2}, map[string]any{"a": 1})
		if len(entries) != 1 {
			t.Fatalf("want 1 entry, got %d", len(entries))
		}
		e := entries[0]
		if e.Kind != KindRemoved || e.PathStr() != "b" {
			t.Fatalf("want removed at b, got %s at %s", e.Kind, e.PathStr())
		}
		if e.Old != int64(2) {
			t.Errorf("Old = %v, want int64(2)", e.Old)
		}
		if e.New != nil {
			t.Errorf("New = %v, want nil", e.New)
		}
	})

	t.Run("added nested key", func(t *testing.T) {
		entries := Compute(
			map[string]any{"spec": map[string]any{"replicas": 1}},
			map[string]any{"spec": map[string]any{"replicas": 1, "image": "nginx"}},
		)
		if len(entries) != 1 {
			t.Fatalf("want 1 entry, got %d", len(entries))
		}
		if entries[0].PathStr() != "spec.image" || entries[0].Kind != KindAdded {
			t.Fatalf("want added at spec.image, got %s at %s", entries[0].Kind, entries[0].PathStr())
		}
	})

	t.Run("empty vs populated", func(t *testing.T) {
		entries := Compute(map[string]any{}, map[string]any{"a": 1, "b": 2})
		if len(entries) != 2 {
			t.Fatalf("want 2 entries, got %d", len(entries))
		}
		for _, e := range entries {
			if e.Kind != KindAdded {
				t.Errorf("%s: kind = %s, want added", e.PathStr(), e.Kind)
			}
		}
	})
}

func TestComputeChangedValues(t *testing.T) {
	t.Run("scalar change", func(t *testing.T) {
		entries := Compute(map[string]any{"replicas": 2}, map[string]any{"replicas": 3})
		if len(entries) != 1 || entries[0].Kind != KindChanged {
			t.Fatalf("want one changed entry, got %+v", entries)
		}
		if entries[0].Old != int64(2) || entries[0].New != int64(3) {
			t.Errorf("got %v → %v, want 2 → 3", entries[0].Old, entries[0].New)
		}
	})

	t.Run("deeply nested change", func(t *testing.T) {
		wrap := func(v string) any {
			return map[string]any{"spec": map[string]any{"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": v}}}}}
		}
		entries := Compute(wrap("v1"), wrap("v2"))
		if len(entries) != 1 {
			t.Fatalf("want 1 entry, got %d", len(entries))
		}
		if got := entries[0].PathStr(); got != "spec.template.metadata.labels.app" {
			t.Fatalf("path = %q", got)
		}
	})

	t.Run("list index appears in path", func(t *testing.T) {
		entries := Compute(
			map[string]any{"users": []any{"alice", "bob"}},
			map[string]any{"users": []any{"alice", "carol"}},
		)
		if !paths(entries)["users[1]"] {
			t.Fatalf("want users[1] in %v", paths(entries))
		}
	})
}

func TestComputeTypeChanges(t *testing.T) {
	cases := []struct {
		name string
		a, b any
	}{
		{"'80' vs 8080", map[string]any{"port": "80"}, map[string]any{"port": 8080}},
		{"map vs list",
			map[string]any{"data": map[string]any{"key": "val"}},
			map[string]any{"data": []any{"item"}}},
		{"null vs string", map[string]any{"key": nil}, map[string]any{"key": "value"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := Compute(tc.a, tc.b)
			if len(entries) != 1 {
				t.Fatalf("want 1 entry, got %d: %+v", len(entries), entries)
			}
			if entries[0].Kind != KindTypeChanged {
				t.Fatalf("kind = %s, want type_changed", entries[0].Kind)
			}
		})
	}
}

func TestComputeLists(t *testing.T) {
	t.Run("item added", func(t *testing.T) {
		entries := Compute(
			map[string]any{"items": []any{"a", "b"}},
			map[string]any{"items": []any{"a", "b", "c"}},
		)
		if !hasKind(entries, KindAdded) {
			t.Fatalf("want an added entry, got %+v", entries)
		}
	})

	t.Run("item removed", func(t *testing.T) {
		entries := Compute(
			map[string]any{"items": []any{"a", "b", "c"}},
			map[string]any{"items": []any{"a", "b"}},
		)
		if !hasKind(entries, KindRemoved) {
			t.Fatalf("want a removed entry, got %+v", entries)
		}
	})
}

func hasKind(entries []Entry, kind Kind) bool {
	for _, e := range entries {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func TestPathStr(t *testing.T) {
	cases := []struct {
		path []any
		want string
	}{
		{[]any{"spec"}, "spec"},
		{[]any{"spec", "template", "labels"}, "spec.template.labels"},
		{[]any{"users", 2, "name"}, "users[2].name"},
		{[]any{"containers", 0}, "containers[0]"},
		{nil, "(root)"},
		{[]any{}, "(root)"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			e := Entry{Path: tc.path, Kind: KindChanged}
			if got := e.PathStr(); got != tc.want {
				t.Fatalf("PathStr() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Paths are built by appending to a shared slice, so a regression here would
// show up as sibling entries reporting each other's paths.
func TestPathsDoNotAlias(t *testing.T) {
	entries := Compute(
		map[string]any{"a": map[string]any{"x": 1, "y": 1}},
		map[string]any{"a": map[string]any{"x": 2, "y": 2}},
	)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	got := paths(entries)
	for _, want := range []string{"a.x", "a.y"} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}
}
