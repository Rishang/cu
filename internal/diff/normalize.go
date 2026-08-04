package diff

import (
	"encoding/json"
	"fmt"
	"math"
)

// normalize canonicalizes parsed config data so values coming from different
// file formats compare cleanly:
//
//   - every map becomes map[string]any (YAML can yield non-string keys)
//   - every number becomes int64 when integral, float64 otherwise
//
// The numeric cases cover what the parsers in loader.go actually hand back:
// json.Number from JSON and HCL, int64/uint64/float64 from YAML and TOML, plus
// plain int for values built in Go. Anything else passes through untouched.
//
// Key ordering is deliberately not part of this: Go maps are unordered and the
// engine iterates keys sorted, which is what the Python version's key sorting
// was actually for.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalize(val)
		}
		return out
	case json.Number:
		// Mirrors Python's json module: "42" is an int, "2.0" is a float.
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, err := t.Float64()
		if err != nil {
			return t.String()
		}
		return f
	case float64:
		// NaN != NaN, so a file compared against itself would diff forever, and
		// neither NaN nor ±Inf has a JSON literal to render. Their YAML spelling
		// ("NaN", "+Inf", "-Inf") compares and prints.
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return fmt.Sprint(t)
		}
		return t
	case int:
		return int64(t)
	case int64:
		return t
	case uint64:
		// YAML hands back positive integers as uint64, which can overflow int64.
		if t <= math.MaxInt64 {
			return int64(t)
		}
		return float64(t)
	default:
		return v
	}
}
