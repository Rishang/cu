package diff

import (
	"sort"
	"testing"
)

func entry(path []any, kind Kind, old, new any) Entry {
	return Entry{Path: path, Kind: kind, Old: old, New: new}
}

func changed(path []any, old, new any) Entry {
	return entry(path, KindChanged, old, new)
}

func keptPaths(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.PathStr())
	}
	sort.Strings(out)
	return out
}

func TestApplyIgnoreKeys(t *testing.T) {
	cases := []struct {
		name       string
		entries    []Entry
		rules      FilterRules
		wantKept   int
		wantPathIs string
	}{
		{"exact segment match",
			[]Entry{changed([]any{"spec", "metadata", "creationTimestamp"}, nil, nil)},
			FilterRules{LocalIgnoreKeys: []string{"metadata"}}, 0, ""},
		{"root segment",
			[]Entry{changed([]any{"data", "password"}, nil, nil)},
			FilterRules{LocalIgnoreKeys: []string{"data"}}, 0, ""},
		{"deep segment",
			[]Entry{changed([]any{"a", "b", "c", "status", "d"}, nil, nil)},
			FilterRules{LocalIgnoreKeys: []string{"status"}}, 0, ""},
		{"no match keeps entry",
			[]Entry{changed([]any{"spec", "replicas"}, nil, nil)},
			FilterRules{LocalIgnoreKeys: []string{"metadata"}}, 1, "spec.replicas"},
		{"substring does not match",
			[]Entry{changed([]any{"metadata"}, nil, nil)},
			FilterRules{LocalIgnoreKeys: []string{"meta"}}, 1, "metadata"},
		{"numeric index matches its string form",
			[]Entry{changed([]any{"users", 0, "name"}, nil, nil)},
			FilterRules{LocalIgnoreKeys: []string{"0"}}, 0, ""},
		{"global rules apply",
			[]Entry{changed([]any{"status", "phase"}, nil, nil)},
			FilterRules{GlobalIgnoreKeys: []string{"status"}}, 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, ignored := Apply(tc.entries, tc.rules)
			if len(kept) != tc.wantKept {
				t.Fatalf("kept %d entries, want %d (%v)", len(kept), tc.wantKept, keptPaths(kept))
			}
			if len(kept)+len(ignored) != len(tc.entries) {
				t.Errorf("entries lost: kept %d + ignored %d != %d",
					len(kept), len(ignored), len(tc.entries))
			}
			if tc.wantPathIs != "" && kept[0].PathStr() != tc.wantPathIs {
				t.Errorf("kept %q, want %q", kept[0].PathStr(), tc.wantPathIs)
			}
		})
	}
}

func TestApplyGlobalAndLocalKeysCombine(t *testing.T) {
	entries := []Entry{
		changed([]any{"metadata", "uid"}, nil, nil),
		changed([]any{"status", "ready"}, nil, nil),
		changed([]any{"spec", "replicas"}, nil, nil),
	}
	kept, ignored := Apply(entries, FilterRules{
		GlobalIgnoreKeys: []string{"metadata"},
		LocalIgnoreKeys:  []string{"status"},
	})

	if len(kept) != 1 || kept[0].PathStr() != "spec.replicas" {
		t.Fatalf("kept = %v, want [spec.replicas]", keptPaths(kept))
	}
	if len(ignored) != 2 {
		t.Fatalf("ignored %d entries, want 2", len(ignored))
	}
}

func TestApplyIgnorePatterns(t *testing.T) {
	cases := []struct {
		name     string
		entry    Entry
		patterns []string
		wantKept bool
	}{
		{"only old side matches",
			changed([]any{"cluster"}, "my-dev-cluster", "prod-cluster"), []string{"dev"}, true},
		{"only new side matches",
			changed([]any{"db"}, "prod-db", "stage-db"), []string{"stage"}, true},
		{"case-insensitive, one side",
			changed([]any{"env"}, "TEST_DB", "PROD_DB"), []string{"test"}, true},
		{"identical values are ignored",
			changed([]any{"env"}, "dev-app", "dev-app"), []string{"dev"}, false},
		{"both sides differ only by token",
			changed([]any{"url"}, "https://dev.example.com", "https://prod.example.com"),
			[]string{"dev", "prod"}, false},
		{"substring only on one side",
			changed([]any{"url"}, "https://dev.example.com", "https://prod.example.com"),
			[]string{"dev"}, true},
		{"pattern matches nothing",
			changed([]any{"replicas"}, 2, 3), []string{"dev"}, true},
		{"remaining text differs",
			changed([]any{"x"}, "dev-alpha-backend", "prod-completely-different-thing"),
			[]string{"dev", "prod"}, true},
		{"no word boundary",
			changed([]any{"key"}, "mydevserver", "myprodserver"), []string{"dev", "prod"}, true},
		{"hosts differing only by env",
			changed([]any{"host"}, "dev-server", "prod-server"), []string{"dev", "prod"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, ignored := Apply([]Entry{tc.entry},
				FilterRules{LocalIgnorePatterns: tc.patterns})

			if tc.wantKept && len(kept) != 1 {
				t.Fatalf("entry was ignored, expected it kept")
			}
			if !tc.wantKept && len(ignored) != 1 {
				t.Fatalf("entry was kept, expected it ignored")
			}
		})
	}
}

// Added and removed entries have no counterpart to compare against, so no
// pattern should ever suppress them.
func TestApplyNeverIgnoresAddedOrRemoved(t *testing.T) {
	cases := []Entry{
		entry([]any{"ns"}, KindAdded, nil, "dev-namespace"),
		entry([]any{"ns"}, KindRemoved, "prod-namespace", nil),
	}
	for _, e := range cases {
		t.Run(string(e.Kind), func(t *testing.T) {
			kept, ignored := Apply([]Entry{e},
				FilterRules{LocalIgnorePatterns: []string{"dev", "prod"}})
			if len(kept) != 1 || len(ignored) != 0 {
				t.Fatalf("kept %d, ignored %d — want 1 kept", len(kept), len(ignored))
			}
		})
	}
}

func TestApplyMixedPatterns(t *testing.T) {
	entries := []Entry{
		changed([]any{"a"}, "dev-cluster", "prod-cluster"),                 // ignored
		changed([]any{"b"}, "dev-api.company.com", "prod-api.company.com"), // ignored
		changed([]any{"c"}, "dev-cluster", "completely-different"),         // kept
		changed([]any{"d"}, "normal", "other"),                             // kept
	}
	kept, ignored := Apply(entries, FilterRules{LocalIgnorePatterns: []string{"dev", "prod"}})

	if got := keptPaths(kept); len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("kept = %v, want [c d]", got)
	}
	if len(ignored) != 2 {
		t.Fatalf("ignored %d, want 2", len(ignored))
	}
}

func TestApplyEmptyInputs(t *testing.T) {
	t.Run("no rules keeps everything", func(t *testing.T) {
		entries := []Entry{changed([]any{"a"}, nil, nil), changed([]any{"b"}, nil, nil)}
		kept, ignored := Apply(entries, FilterRules{})
		if len(kept) != 2 || len(ignored) != 0 {
			t.Fatalf("kept %d, ignored %d", len(kept), len(ignored))
		}
	})

	t.Run("no entries", func(t *testing.T) {
		kept, ignored := Apply(nil, FilterRules{
			LocalIgnoreKeys:     []string{"x"},
			LocalIgnorePatterns: []string{"y"},
		})
		if len(kept) != 0 || len(ignored) != 0 {
			t.Fatalf("kept %d, ignored %d", len(kept), len(ignored))
		}
	})
}

func TestQueryPathPrefix(t *testing.T) {
	entries := []Entry{
		changed([]any{"spec", "replicas"}, 1, 2),
		changed([]any{"spec", "template", "image"}, "a", "b"),
		changed([]any{"metadata", "name"}, "x", "y"),
		changed([]any{"spec"}, "flat", "value"),
		changed([]any{"specials", "nope"}, 1, 2),
		changed([]any{"containers", 0, "image"}, "a", "b"),
	}

	cases := []struct {
		query string
		want  []string
	}{
		{"spec", []string{"spec", "spec.replicas", "spec.template.image"}},
		{"spec.", []string{"spec", "spec.replicas", "spec.template.image"}},
		{"spec.template", []string{"spec.template.image"}},
		{"spec.replicas", []string{"spec.replicas"}},
		{"metadata", []string{"metadata.name"}},
		{"containers", []string{"containers[0].image"}},
		{"nothing", nil},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got, err := Query(entries, tc.query)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			assertPaths(t, got, tc.want)
		})
	}
}

func TestQueryJMESPath(t *testing.T) {
	entries := []Entry{
		changed([]any{"spec", "replicas"}, int64(1), int64(2)),
		entry([]any{"spec", "extra"}, KindAdded, nil, "new"),
		entry([]any{"spec", "gone"}, KindRemoved, "old", nil),
		changed([]any{"image"}, "t2.micro", "t3.small"),
	}

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"by kind", "[?kind=='changed']", []string{"image", "spec.replicas"}},
		{"negated kind", "[?kind!='changed']", []string{"spec.extra", "spec.gone"}},
		{"contains on path", "[?contains(path, 'spec')]",
			[]string{"spec.extra", "spec.gone", "spec.replicas"}},
		{"exact path", "[?path=='image']", []string{"image"}},
		{"by old value", "[?old=='t2.micro']", []string{"image"}},
		{"matches nothing", "[?kind=='nonexistent']", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Query(entries, tc.query)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			assertPaths(t, got, tc.want)
		})
	}
}

func TestQueryInvalidExpression(t *testing.T) {
	_, err := Query([]Entry{changed([]any{"a"}, 1, 2)}, "[?kind==")
	assertErrContains(t, err, "invalid JMESPath query")
}

func assertPaths(t *testing.T, entries []Entry, want []string) {
	t.Helper()
	got := keptPaths(entries)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSortByPathDoesNotMutateInput(t *testing.T) {
	entries := []Entry{changed([]any{"z"}, nil, nil), changed([]any{"a"}, nil, nil)}
	sorted := SortByPath(entries)

	if entries[0].PathStr() != "z" {
		t.Errorf("input was reordered: %v", keptPaths(entries))
	}
	if sorted[0].PathStr() != "a" {
		t.Errorf("output not sorted: %v", keptPaths(sorted))
	}
}
