package diff

import (
	"fmt"
	"slices"
	"strings"

	jmespath "github.com/jmespath-community/go-jmespath"
)

// FilterRules collects the ignore rules applied to one diff pair. Global rules
// come from the config file, local rules from the individual diff entry.
type FilterRules struct {
	GlobalIgnoreKeys     []string
	LocalIgnoreKeys      []string
	GlobalIgnorePatterns []string
	LocalIgnorePatterns  []string
}

// Apply splits entries into kept and ignored using every configured rule.
// First match suppresses: ignore keys before ignore patterns.
func Apply(entries []Entry, r FilterRules) (kept, ignored []Entry) {
	keys := slices.Concat(r.GlobalIgnoreKeys, r.LocalIgnoreKeys)
	compiled := compilePatterns(slices.Concat(r.GlobalIgnorePatterns, r.LocalIgnorePatterns))

	for _, e := range entries {
		switch {
		case len(keys) > 0 && pathHasKey(e, keys):
			ignored = append(ignored, e)
		case len(compiled) > 0 && smartIgnore(compiled, e):
			ignored = append(ignored, e)
		default:
			kept = append(kept, e)
		}
	}
	return kept, ignored
}

// smartIgnore strips the configured tokens from both sides and ignores the
// entry when what remains is identical — i.e. the marker token was the only
// difference. Added and removed entries have no counterpart, so they stay.
func smartIgnore(compiled patternSet, e Entry) bool {
	if e.Kind == KindAdded || e.Kind == KindRemoved {
		return false
	}
	return valuesEqualAfterStripping(compiled, e.Old, e.New)
}

func pathHasKey(e Entry, ignoreKeys []string) bool {
	for _, seg := range e.Path {
		if slices.Contains(ignoreKeys, fmt.Sprint(seg)) {
			return true
		}
	}
	return false
}

// Query filters entries by a bare path prefix or a JMESPath expression.
//
// Path prefix (does not start with '['):
//
//	spec.replicas                keeps that path and anything nested under it
//
// JMESPath (starts with '['), applied to [{path, kind, old, new}]:
//
//	[?kind=='changed']
//	[?contains(path, 'resources')]
func Query(entries []Entry, query string) ([]Entry, error) {
	if !strings.HasPrefix(strings.TrimLeft(query, " \t"), "[") {
		return prefixFilter(entries, strings.TrimRight(query, ".")), nil
	}

	expr, err := jmespath.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("invalid JMESPath query %q: %w", query, err)
	}

	data := make([]any, 0, len(entries))
	for _, e := range entries {
		data = append(data, map[string]any{
			"path": e.PathStr(),
			"kind": string(e.Kind),
			"old":  e.Old,
			"new":  e.New,
		})
	}

	result, err := expr.Search(data)
	if err != nil {
		return nil, fmt.Errorf("invalid JMESPath query %q: %w", query, err)
	}
	if result == nil {
		return nil, nil
	}

	rows, ok := result.([]any)
	if !ok {
		rows = []any{result}
	}

	keep := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		path, hasPath := m["path"].(string)
		kind, hasKind := m["kind"].(string)
		if hasPath && hasKind {
			keep[path+"\x00"+kind] = struct{}{}
		}
	}

	// A projection like `[].path` or `length(@)` returns rows the filter cannot
	// map back to entries. Silently keeping nothing would read as "no
	// differences", so say what went wrong instead.
	if len(keep) == 0 && len(rows) > 0 {
		return nil, fmt.Errorf("JMESPath query %q must return whole diff entries, not %T values — try a filter like [?kind=='changed']", query, rows[0])
	}

	var out []Entry
	for _, e := range entries {
		if _, ok := keep[e.PathStr()+"\x00"+string(e.Kind)]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func prefixFilter(entries []Entry, prefix string) []Entry {
	var out []Entry
	for _, e := range entries {
		p := e.PathStr()
		if p == prefix || strings.HasPrefix(p, prefix+".") || strings.HasPrefix(p, prefix+"[") {
			out = append(out, e)
		}
	}
	return out
}

// SortByPath orders entries by their rendered path, for stable output.
func sortByPath(entries []Entry) []Entry {
	out := slices.Clone(entries)
	slices.SortStableFunc(out, func(a, b Entry) int {
		return strings.Compare(a.PathStr(), b.PathStr())
	})
	return out
}
