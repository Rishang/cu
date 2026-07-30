package diff

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Kind classifies a single structural difference.
type Kind string

const (
	KindAdded       Kind = "added"
	KindRemoved     Kind = "removed"
	KindChanged     Kind = "changed"
	KindTypeChanged Kind = "type_changed"
)

// Entry is one structural difference at a specific path.
// Path segments are either string (map key) or int (slice index).
type Entry struct {
	Path []any
	Kind Kind
	Old  any
	New  any
}

// PathStr renders a human-readable path, e.g. "spec.containers[0].image".
func (e Entry) PathStr() string {
	var b strings.Builder
	for _, seg := range e.Path {
		if i, ok := seg.(int); ok {
			fmt.Fprintf(&b, "[%d]", i)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(fmt.Sprint(seg))
	}
	if b.Len() == 0 {
		return "(root)"
	}
	return b.String()
}

// Compute normalizes both sides then walks them for structural differences.
func Compute(a, b any) []Entry {
	return diffValues(Normalize(a), Normalize(b), nil)
}

type valKind int

const (
	kindNil valKind = iota
	kindBool
	kindNum
	kindStr
	kindMap
	kindSlice
	kindOther
)

func kindOf(v any) valKind {
	switch v.(type) {
	case nil:
		return kindNil
	case bool:
		return kindBool
	case int64, float64:
		return kindNum
	case string:
		return kindStr
	case map[string]any:
		return kindMap
	case []any:
		return kindSlice
	default:
		return kindOther
	}
}

func diffValues(a, b any, path []any) []Entry {
	ka, kb := kindOf(a), kindOf(b)
	if ka != kb {
		// Same rendered value across types is not a difference — YAML quoting
		// alone should not show up as a diff (port: "8080" vs port: 8080).
		if scalarString(a) == scalarString(b) {
			return nil
		}
		return []Entry{{Path: clonePath(path), Kind: KindTypeChanged, Old: a, New: b}}
	}

	switch ka {
	case kindMap:
		return diffMaps(a.(map[string]any), b.(map[string]any), path)
	case kindSlice:
		return diffSlices(a.([]any), b.([]any), path)
	case kindNum:
		if numEqual(a, b) {
			return nil
		}
	default:
		if a == b {
			return nil
		}
	}
	return []Entry{{Path: clonePath(path), Kind: KindChanged, Old: a, New: b}}
}

func diffMaps(a, b map[string]any, path []any) []Entry {
	union := make(map[string]struct{}, len(a)+len(b))
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			union[k] = struct{}{}
		}
	}

	var entries []Entry
	for _, k := range slices.Sorted(maps.Keys(union)) {
		child := append(clonePath(path), k)
		av, inA := a[k]
		bv, inB := b[k]
		switch {
		case !inA:
			entries = append(entries, Entry{Path: child, Kind: KindAdded, New: bv})
		case !inB:
			entries = append(entries, Entry{Path: child, Kind: KindRemoved, Old: av})
		default:
			entries = append(entries, diffValues(av, bv, child)...)
		}
	}
	return entries
}

func diffSlices(a, b []any, path []any) []Entry {
	var entries []Entry
	for i := 0; i < max(len(a), len(b)); i++ {
		child := append(clonePath(path), i)
		switch {
		case i >= len(a):
			entries = append(entries, Entry{Path: child, Kind: KindAdded, New: b[i]})
		case i >= len(b):
			entries = append(entries, Entry{Path: child, Kind: KindRemoved, Old: a[i]})
		default:
			entries = append(entries, diffValues(a[i], b[i], child)...)
		}
	}
	return entries
}

// clonePath copies a path so appended segments never alias a parent's backing array.
func clonePath(path []any) []any {
	out := make([]any, len(path), len(path)+1)
	copy(out, path)
	return out
}

// numEqual compares numbers across the int64/float64 split, so 1 and 1.0 match.
func numEqual(a, b any) bool {
	ai, aInt := a.(int64)
	bi, bInt := b.(int64)
	if aInt && bInt {
		return ai == bi
	}
	return toFloat(a) == toFloat(b)
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case int64:
		return float64(t)
	case float64:
		return t
	default:
		return 0
	}
}
