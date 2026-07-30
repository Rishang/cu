package diff

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeNumbers(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  any
	}{
		{"int", 42, int64(42)},
		{"int32", int32(7), int64(7)},
		{"uint", uint(9), int64(9)},
		{"int64 passes through", int64(5), int64(5)},
		{"float32 widens", float32(1.5), float64(1.5)},
		{"float64 passes through", 2.5, 2.5},
		{"integral json.Number becomes int", json.Number("42"), int64(42)},
		{"decimal json.Number becomes float", json.Number("2.0"), float64(2)},
		{"exponent json.Number becomes float", json.Number("1e3"), float64(1000)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.input)
			if got != tc.want {
				t.Fatalf("Normalize(%v) = %v (%T), want %v (%T)",
					tc.input, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestNormalizePrimitivesUnchanged(t *testing.T) {
	cases := []any{"hello", true, false, nil}
	for _, input := range cases {
		if got := Normalize(input); got != input {
			t.Errorf("Normalize(%v) = %v, want unchanged", input, got)
		}
	}
}

// YAML can produce non-string keys; the engine only walks map[string]any.
func TestNormalizeCoercesMapKeys(t *testing.T) {
	got := Normalize(map[any]any{"a": 1, 2: "two", true: "yes"})

	want := map[string]any{"a": int64(1), "2": "two", "true": "yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestNormalizeRecurses(t *testing.T) {
	got := Normalize(map[string]any{
		"outer": map[any]any{"inner": []any{1, map[any]any{"deep": float32(2.5)}}},
	})

	want := map[string]any{
		"outer": map[string]any{
			"inner": []any{int64(1), map[string]any{"deep": float64(2.5)}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestNormalizePreservesListOrder(t *testing.T) {
	got := Normalize([]any{"banana", "apple", "cherry"})
	want := []any{"banana", "apple", "cherry"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	people := []any{
		map[string]any{"name": "Bob", "age": 25},
		map[string]any{"name": "Alice", "age": 30},
	}
	normalized, ok := Normalize(people).([]any)
	if !ok {
		t.Fatalf("expected a slice, got %T", Normalize(people))
	}
	if normalized[0].(map[string]any)["name"] != "Bob" {
		t.Errorf("list order changed: %v", normalized)
	}
}

func TestNormalizeStringSlice(t *testing.T) {
	got := Normalize([]string{"a", "b"})
	want := []any{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestNormalizeEmptyStructures(t *testing.T) {
	if got := Normalize(map[string]any{}); !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("empty map = %#v", got)
	}
	if got := Normalize([]any{}); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("empty slice = %#v", got)
	}
}

func TestNormalizeKeepsNulls(t *testing.T) {
	got := Normalize(map[string]any{"key": nil, "other": "val"}).(map[string]any)
	if got["key"] != nil {
		t.Errorf("key = %v, want nil", got["key"])
	}
	if got["other"] != "val" {
		t.Errorf("other = %v, want val", got["other"])
	}
}
