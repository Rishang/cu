package diff

import "testing"

func TestCompilePatternsSplitsCommas(t *testing.T) {
	compiled := CompilePatterns([]string{" dev, PROD ", "stage"})
	if len(compiled) != 3 {
		t.Fatalf("want 3 compiled tokens, got %d", len(compiled))
	}
}

func TestCompilePatternsIgnoresBlanks(t *testing.T) {
	if got := CompilePatterns([]string{"", " , ,"}); len(got) != 0 {
		t.Fatalf("want no tokens, got %d", len(got))
	}
}

func TestValuesEqualAfterStripping(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		left     any
		right    any
		want     bool
	}{
		{"case-insensitive token strip", []string{" dev, PROD "}, "DEV-api", "prod-api", true},
		{"no word boundary means no strip", []string{"dev,prod"}, "mydevapi", "myprodapi", false},
		{"absent value counts as empty", []string{"dev"}, nil, "", true},
		{"absent value against content", []string{"dev"}, nil, "different", false},
		{"identical values always match", []string{"dev"}, "dev-app", "dev-app", true},
		{"remaining text must match", []string{"dev", "prod"},
			"dev-alpha-backend", "prod-completely-different", false},
		{"hostnames differing only by env", []string{"dev", "prod"},
			"https://dev.example.com", "https://prod.example.com", true},
		{"only one side carries the token", []string{"dev"},
			"https://dev.example.com", "https://prod.example.com", false},
		{"numbers stringify", []string{"dev"}, 2, 2, true},
		{"different numbers", []string{"dev"}, 2, 3, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := CompilePatterns(tc.patterns)
			if got := ValuesEqualAfterStripping(compiled, tc.left, tc.right); got != tc.want {
				t.Fatalf("ValuesEqualAfterStripping(%v, %v) = %v, want %v",
					tc.left, tc.right, got, tc.want)
			}
		})
	}
}

// RE2 has no lookaround, so boundaries are enforced after matching. These are
// the cases that logic has to get right.
func TestStripAllBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		input   string
		want    string
	}{
		{"leading token", "dev", "dev-api", "-api"},
		{"trailing token", "dev", "api-dev", "api-"},
		{"middle token", "dev", "api-dev-x", "api--x"},
		{"glued prefix is kept", "dev", "mydev", "mydev"},
		{"glued suffix is kept", "dev", "devops", "devops"},
		{"glued both sides is kept", "dev", "mydevops", "mydevops"},
		{"digit boundary is kept", "dev", "dev1", "dev1"},
		{"dot is a boundary", "dev", "dev.example.com", ".example.com"},
		{"repeated occurrences", "dev", "dev/dev", "/"},
		{"whole value", "dev", "dev", ""},
		{"case insensitive", "dev", "DEV-api", "-api"},
		{"unrelated text untouched", "dev", "prod-api", "prod-api"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := CompilePatterns([]string{tc.pattern})
			if got := StripAll(compiled, tc.input); got != tc.want {
				t.Fatalf("StripAll(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// Tokens are matched literally, so regex metacharacters must not be special.
func TestStripAllTreatsTokensAsLiterals(t *testing.T) {
	compiled := CompilePatterns([]string{"a.c"})
	if got := StripAll(compiled, "abc"); got != "abc" {
		t.Errorf("StripAll(abc) = %q, want abc unchanged", got)
	}
	if got := StripAll(compiled, "a.c-x"); got != "-x" {
		t.Errorf("StripAll(a.c-x) = %q, want -x", got)
	}
}

// Multibyte input must not be sliced mid-rune.
func TestStripAllHandlesUnicode(t *testing.T) {
	compiled := CompilePatterns([]string{"dev"})
	if got := StripAll(compiled, "héllo-dev-wörld"); got != "héllo--wörld" {
		t.Fatalf("got %q", got)
	}
}
