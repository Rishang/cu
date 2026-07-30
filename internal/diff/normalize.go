package diff

import (
	"encoding/json"
	"fmt"
	"math"
)

// Normalize canonicalizes parsed config data so values coming from different
// file formats compare cleanly:
//
//   - every map becomes map[string]any (YAML can yield non-string keys)
//   - every slice becomes []any
//   - every number becomes int64 when integral, float64 otherwise
//
// Key ordering is deliberately not part of this: Go maps are unordered and the
// engine iterates keys sorted, which is what the Python version's key sorting
// was actually for.
func Normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = Normalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = Normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = Normalize(val)
		}
		return out
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
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
	case int:
		return int64(t)
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case int64:
		return t
	case uint:
		return uintToNum(uint64(t))
	case uint8:
		return int64(t)
	case uint16:
		return int64(t)
	case uint32:
		return int64(t)
	case uint64:
		return uintToNum(t)
	case float32:
		return float64(t)
	default:
		return v
	}
}

func uintToNum(u uint64) any {
	if u <= math.MaxInt64 {
		return int64(u)
	}
	return float64(u)
}
