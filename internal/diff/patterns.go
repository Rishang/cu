package diff

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// patternSet holds ignore_patterns tokens as case-insensitive literal matchers.
type patternSet []*regexp.Regexp

// compilePatterns splits comma-separated tokens and compiles each as a
// case-insensitive literal. Word boundaries are enforced at match time rather
// than in the pattern: RE2 has no lookaround, so `(?<![A-Za-z0-9])` from the
// Python version cannot be expressed here.
func compilePatterns(patterns []string) patternSet {
	var compiled patternSet
	for _, pattern := range patterns {
		for token := range strings.SplitSeq(pattern, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			compiled = append(compiled, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(token)))
		}
	}
	return compiled
}

// stripAll removes every configured token from s, one pattern at a time.
func stripAll(c patternSet, s string) string {
	for _, re := range c {
		s = stripBounded(re, s)
	}
	return s
}

// valuesEqualAfterStripping reports whether both values are identical once the
// configured tokens are removed. Absent values count as empty.
//
// ponytail: exact equality — the Python threshold was a hardcoded 1.0, and
// SequenceMatcher.ratio() >= 1.0 is true only for identical strings. If a
// fuzzy threshold ever becomes configurable, swap in a real ratio here.
func valuesEqualAfterStripping(c patternSet, left, right any) bool {
	return stripAll(c, scalarString(left)) == stripAll(c, scalarString(right))
}

// stripBounded deletes matches that are not glued to an adjacent alphanumeric,
// so "dev" clears "dev-api" but leaves "mydevserver" alone.
func stripBounded(re *regexp.Regexp, s string) string {
	locs := re.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		if !atBoundary(s, loc[0], loc[1]) {
			continue
		}
		b.WriteString(s[last:loc[0]])
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

func atBoundary(s string, start, end int) bool {
	if start > 0 {
		if r, _ := utf8.DecodeLastRuneInString(s[:start]); isAlnum(r) {
			return false
		}
	}
	if end < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[end:]); isAlnum(r) {
			return false
		}
	}
	return true
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// scalarString renders a value for string comparison; absent values are empty.
func scalarString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
