package diff

import (
	"fmt"
	"slices"
	"strings"
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

// Query keeps the entries at or under a dotted path prefix:
//
//	spec.replicas                that path and anything nested under it
//
// Anything richer belongs in jq, which --format json feeds directly.
func Query(entries []Entry, query string) []Entry {
	prefix := strings.TrimRight(strings.TrimSpace(query), ".")
	var out []Entry
	for _, e := range entries {
		p := e.PathStr()
		if p == prefix || strings.HasPrefix(p, prefix+".") || strings.HasPrefix(p, prefix+"[") {
			out = append(out, e)
		}
	}
	return out
}

// sortByPath orders entries by their rendered path, for stable output.
func sortByPath(entries []Entry) []Entry {
	out := slices.Clone(entries)
	slices.SortStableFunc(out, func(a, b Entry) int {
		return strings.Compare(a.PathStr(), b.PathStr())
	})
	return out
}
