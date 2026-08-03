package diff

import (
	"reflect"
	"slices"
	"testing"

	yaml "github.com/goccy/go-yaml"
)

// ignore_patterns accepts a comma-separated string or a list, and any list item
// may itself be comma-separated.
func TestPatternsUnmarshal(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Patterns
	}{
		{"comma-separated string", "qa,prod", Patterns{"qa", "prod"}},
		{"single token", "qa", Patterns{"qa"}},
		{"whitespace is trimmed", "' qa , prod '", Patterns{"qa", "prod"}},
		{"yaml list", "[dev, stage]", Patterns{"dev", "stage"}},
		{"list items may hold commas", "['a,b', c]", Patterns{"a", "b", "c"}},
		{"empty tokens dropped", "',,qa,'", Patterns{"qa"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := "global_ignore_patterns: " + tc.input + "\n" +
				"diffs:\n  - files: [a.yaml, b.yaml]\n"

			cfg := &Config{}
			if err := yaml.Unmarshal([]byte(doc), cfg); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(cfg.GlobalIgnorePatterns, tc.want) {
				t.Fatalf("got %#v, want %#v", cfg.GlobalIgnorePatterns, tc.want)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"valid", Config{Format: FormatTable,
			Diffs: []Diff{{Files: []string{"a.yaml", "b.yaml"}}}}, ""},
		{"three files is valid", Config{Format: FormatJSON,
			Diffs: []Diff{{Files: []string{"a.yaml", "b.yaml", "c.yaml"}}}}, ""},
		{"no diffs", Config{Format: FormatTable},
			"'diffs' must contain at least one entry"},
		{"one file", Config{Format: FormatTable,
			Diffs: []Diff{{Files: []string{"a.yaml"}}}},
			"'files' requires at least 2 entries, got 1"},
		{"no files", Config{Format: FormatTable, Diffs: []Diff{{}}},
			"'files' requires at least 2 entries, got 0"},
		{"bad format", Config{Format: "yaml",
			Diffs: []Diff{{Files: []string{"a.yaml", "b.yaml"}}}},
			"invalid format"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			assertErrContains(t, err, tc.wantErr)
		})
	}
}

func TestValidFormat(t *testing.T) {
	for _, f := range []Format{FormatUnified, FormatTable, FormatJSON} {
		if !ValidFormat(f) {
			t.Errorf("ValidFormat(%q) = false", f)
		}
	}
	for _, f := range []Format{"", "yaml", "TABLE", "unified "} {
		if ValidFormat(f) {
			t.Errorf("ValidFormat(%q) = true", f)
		}
	}
}

func TestLoadConfigFromAsset(t *testing.T) {
	cfg, err := LoadConfig(asset("cu_diff.yml"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if len(cfg.Diffs) != 1 {
		t.Fatalf("got %d diff entries, want 1", len(cfg.Diffs))
	}
	if len(cfg.Diffs[0].Files) != 3 {
		t.Fatalf("got %d files, want 3", len(cfg.Diffs[0].Files))
	}
	if cfg.Format != FormatTable {
		t.Errorf("format = %q, want table", cfg.Format)
	}
	if !slices.Contains(cfg.GlobalIgnorePatterns, "dev") {
		t.Errorf("global_ignore_patterns = %v, want it to contain dev", cfg.GlobalIgnorePatterns)
	}
}

func TestLoadConfigDefaultsToTable(t *testing.T) {
	path := write(t, "cu_diff.yml", "diffs:\n  - files: [a.yaml, b.yaml]\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Format != FormatTable {
		t.Fatalf("format = %q, want table", cfg.Format)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := LoadConfig("/nonexistent/cu_diff.yml")
		assertErrContains(t, err, "config file not found")
	})

	t.Run("validation runs on load", func(t *testing.T) {
		path := write(t, "cu_diff.yml", "diffs:\n  - files: [only-one.yaml]\n")
		_, err := LoadConfig(path)
		assertErrContains(t, err, "at least 2 entries")
	})
}

// The schema is hand-written, so the risk is a field being added to Config or
// Diff and nobody documenting it. Walk the struct tags and demand a property
// for each — this is the guard that lets the schema stay a literal.
func TestSchemaCoversEveryField(t *testing.T) {
	raw, err := SchemaYAML()
	if err != nil {
		t.Fatalf("SchemaYAML: %v", err)
	}

	// It must round-trip as YAML, since agents are told to parse it.
	var schema struct {
		Properties map[string]struct {
			Items struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"items"`
		} `yaml:"properties"`
		Example map[string]any `yaml:"example"`
	}
	if err := yaml.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not valid YAML: %v", err)
	}
	if len(schema.Example) == 0 {
		t.Error("schema carries no usage example")
	}

	for _, tc := range []struct {
		what       string
		typ        reflect.Type
		properties map[string]bool
	}{
		{"Config", reflect.TypeFor[Config](), keySet(schema.Properties)},
		{"Diff", reflect.TypeFor[Diff](), keySet(schema.Properties["diffs"].Items.Properties)},
	} {
		for i := range tc.typ.NumField() {
			name := tc.typ.Field(i).Tag.Get("yaml")
			if !tc.properties[name] {
				t.Errorf("%s field %q has no schema property — document it in schemaDoc", tc.what, name)
			}
		}
	}
}

func keySet[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}
