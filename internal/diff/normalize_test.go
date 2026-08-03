package diff

import (
	"encoding/json"
	"math"
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
		{"int64 passes through", int64(5), int64(5)},
		{"uint64 from YAML", uint64(9), int64(9)},
		{"uint64 past int64 widens to float", uint64(math.MaxUint64), float64(math.MaxUint64)},
		{"float64 passes through", 2.5, 2.5},
		{"integral json.Number becomes int", json.Number("42"), int64(42)},
		{"decimal json.Number becomes float", json.Number("2.0"), float64(2)},
		{"exponent json.Number becomes float", json.Number("1e3"), float64(1000)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalize(tc.input)
			if got != tc.want {
				t.Fatalf("normalize(%v) = %v (%T), want %v (%T)",
					tc.input, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestNormalizePrimitivesUnchanged(t *testing.T) {
	cases := []any{"hello", true, false, nil}
	for _, input := range cases {
		if got := normalize(input); got != input {
			t.Errorf("normalize(%v) = %v, want unchanged", input, got)
		}
	}
}

// YAML can produce non-string keys; the engine only walks map[string]any.
func TestNormalizeCoercesMapKeys(t *testing.T) {
	got := normalize(map[any]any{"a": 1, 2: "two", true: "yes"})

	want := map[string]any{"a": int64(1), "2": "two", "true": "yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestNormalizeRecurses(t *testing.T) {
	got := normalize(map[string]any{
		"outer": map[any]any{"inner": []any{1, map[any]any{"deep": 2.5}}},
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
	got := normalize([]any{"banana", "apple", "cherry"})
	want := []any{"banana", "apple", "cherry"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	people := []any{
		map[string]any{"name": "Bob", "age": 25},
		map[string]any{"name": "Alice", "age": 30},
	}
	normalized, ok := normalize(people).([]any)
	if !ok {
		t.Fatalf("expected a slice, got %T", normalize(people))
	}
	if normalized[0].(map[string]any)["name"] != "Bob" {
		t.Errorf("list order changed: %v", normalized)
	}
}

func TestNormalizeEmptyStructures(t *testing.T) {
	if got := normalize(map[string]any{}); !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("empty map = %#v", got)
	}
	if got := normalize([]any{}); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("empty slice = %#v", got)
	}
}

func TestNormalizeKeepsNulls(t *testing.T) {
	got := normalize(map[string]any{"key": nil, "other": "val"}).(map[string]any)
	if got["key"] != nil {
		t.Errorf("key = %v, want nil", got["key"])
	}
	if got["other"] != "val" {
		t.Errorf("other = %v, want val", got["other"])
	}
}
