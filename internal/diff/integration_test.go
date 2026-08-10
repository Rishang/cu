package diff

import (
	"strings"
	"testing"
)

// envPatterns are the tokens the shared cu_diff.yml fixture ignores.
var envPatterns = []string{"dev", "stage", "prod"}

func loadAsset(t *testing.T, name string) any {
	t.Helper()
	data, err := LoadFile(asset(name))
	if err != nil {
		t.Fatalf("LoadFile(%s): %v", name, err)
	}
	return data
}

// Hostnames that differ only by environment name should be suppressed, while
// genuinely structural differences survive.
func TestSmartIgnoreSuppressesEnvHostnames(t *testing.T) {
	entries := Compute(loadAsset(t, "app-dev.yaml"), loadAsset(t, "app-prod.yaml"))
	kept, ignored := Apply(entries, FilterRules{GlobalIgnorePatterns: envPatterns})

	ignoredPaths := paths(ignored)
	for _, want := range []string{"app.host", "database.host"} {
		if !ignoredPaths[want] {
			t.Errorf("%s should have been ignored, ignored = %v", want, ignoredPaths)
		}
	}

	keptPaths := paths(kept)
	for _, want := range []string{"app.replicas", "database.pool_size"} {
		if !keptPaths[want] {
			t.Errorf("%s should have been kept, kept = %v", want, keptPaths)
		}
	}
}

func TestNWayAllPairsDifferAndFilterCleanly(t *testing.T) {
	names := []string{"app-dev.yaml", "app-stage.yaml", "app-prod.yaml"}

	pairs := 0
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			pairs++
			t.Run(names[i]+" vs "+names[j], func(t *testing.T) {
				entries := Compute(loadAsset(t, names[i]), loadAsset(t, names[j]))
				if len(entries) == 0 {
					t.Fatal("expected differences")
				}

				kept, _ := Apply(entries, FilterRules{GlobalIgnorePatterns: envPatterns})
				for _, e := range kept {
					if strings.Contains(e.PathStr(), "host") {
						t.Errorf("%s should have been ignored", e.PathStr())
					}
				}
				if len(kept) == 0 {
					t.Error("filtering removed every difference")
				}
			})
		}
	}

	if pairs != 3 {
		t.Fatalf("compared %d pairs, want 3 for a 3-way diff", pairs)
	}
}

// A config file drives the same pipeline the CLI uses.
func TestConfigDrivenDiffPipeline(t *testing.T) {
	cfg, err := LoadConfig(asset("cu_diff.yml"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	entry := cfg.Diffs[0]
	a := loadAsset(t, entry.Files[0])
	b := loadAsset(t, entry.Files[2])

	kept, ignored := Apply(Compute(a, b), FilterRules{
		GlobalIgnoreKeys:     cfg.GlobalIgnoreKeys,
		LocalIgnoreKeys:      entry.IgnoreKeys,
		GlobalIgnorePatterns: cfg.GlobalIgnorePatterns,
		LocalIgnorePatterns:  entry.IgnorePatterns,
	})

	if len(ignored) != 2 {
		t.Errorf("ignored %d entries, want 2 (the two hostnames)", len(ignored))
	}
	if len(kept) != 2 {
		t.Errorf("kept %d entries, want 2 (replicas and pool_size)", len(kept))
	}
}

// Cross-format comparison: the same data as JSON and YAML must not report
// spurious differences from parser type choices.
func TestCrossFormatComparison(t *testing.T) {
	entries := Compute(loadAsset(t, "config-a.json"), loadAsset(t, "config-b.yaml"))

	got := paths(entries)
	// Both files set port 5432 as a bare number, and dark_mode true.
	for _, unwanted := range []string{"database.port", "features.dark_mode", "app.name"} {
		if got[unwanted] {
			t.Errorf("%s reported as different but both files agree", unwanted)
		}
	}
	for _, want := range []string{"app.version", "database.ssl", "features.beta"} {
		if !got[want] {
			t.Errorf("missing expected difference %q in %v", want, got)
		}
	}
}

func TestQueryAgainstRealDiff(t *testing.T) {
	entries := Compute(loadAsset(t, "sparse.json"), loadAsset(t, "config-a.json"))

	byPrefix := Query(entries, "database")
	if len(byPrefix) == 0 {
		t.Fatal("prefix query returned nothing")
	}
	for _, e := range byPrefix {
		if !strings.HasPrefix(e.PathStr(), "database") {
			t.Errorf("prefix query leaked %q", e.PathStr())
		}
	}
}
