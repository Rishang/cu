package diff

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	yaml "github.com/goccy/go-yaml"
	"github.com/invopop/jsonschema"
)

// Format is an output format for rendered diffs.
type Format string

const (
	FormatUnified Format = "unified"
	FormatTable   Format = "table"
	FormatJSON    Format = "json"

	// DefaultFormat applies when neither the CLI nor the config file picks one.
	DefaultFormat = FormatTable
)

// ValidFormat reports whether f is a known output format.
func ValidFormat(f Format) bool {
	switch f {
	case FormatUnified, FormatTable, FormatJSON:
		return true
	}
	return false
}

// Patterns accepts either a YAML list or a comma-separated string, so both
// `ignore_patterns: qa,prod` and `ignore_patterns: [qa, prod]` parse.
type Patterns []string

// UnmarshalYAML implements goccy/go-yaml's BytesUnmarshaler.
func (p *Patterns) UnmarshalYAML(b []byte) error {
	var single string
	if err := yaml.Unmarshal(b, &single); err == nil {
		*p = splitTokens([]string{single})
		return nil
	}

	var list []any
	if err := yaml.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("ignore_patterns must be a string or a list: %w", err)
	}
	raw := make([]string, 0, len(list))
	for _, item := range list {
		raw = append(raw, fmt.Sprint(item))
	}
	*p = splitTokens(raw)
	return nil
}

func splitTokens(values []string) Patterns {
	var out Patterns
	for _, v := range values {
		for token := range strings.SplitSeq(v, ",") {
			if token = strings.TrimSpace(token); token != "" {
				out = append(out, token)
			}
		}
	}
	return out
}

// Diff is one comparison entry: two or more files plus its local ignore rules.
type Diff struct {
	Files          []string `json:"files"                     yaml:"files"`
	IgnoreKeys     []string `json:"ignore_keys,omitempty"     yaml:"ignore_keys"`
	IgnorePatterns Patterns `json:"ignore_patterns,omitempty" yaml:"ignore_patterns"`
	Query          string   `json:"query,omitempty"           yaml:"query"`
}

// Config is the cu_diff.yml file format.
type Config struct {
	Format               Format   `json:"format,omitempty"                 yaml:"format"`
	Query                string   `json:"query,omitempty"                  yaml:"query"`
	GlobalIgnoreKeys     []string `json:"global_ignore_keys,omitempty"     yaml:"global_ignore_keys"`
	GlobalIgnorePatterns Patterns `json:"global_ignore_patterns,omitempty" yaml:"global_ignore_patterns"`
	Diffs                []Diff   `json:"diffs"                            yaml:"diffs"`
}

// LoadConfig reads and validates a cu_diff.yml file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.Format == "" {
		cfg.Format = DefaultFormat
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate enforces the same rules the Pydantic models did.
func (c *Config) Validate() error {
	if !ValidFormat(c.Format) {
		return fmt.Errorf("invalid format %q: expected unified, table, or json", c.Format)
	}
	if len(c.Diffs) == 0 {
		return fmt.Errorf("'diffs' must contain at least one entry")
	}
	for i, d := range c.Diffs {
		if len(d.Files) < 2 {
			return fmt.Errorf("diffs[%d]: 'files' requires at least 2 entries, got %d", i, len(d.Files))
		}
	}
	return nil
}

// JSONSchemaExtend documents the Diff fields for --print-schema consumers.
func (Diff) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"files": "Two or more file paths to compare (JSON, YAML, TOML, or HCL). " +
			"When 3+ files are given, all N-choose-2 pairs are compared. " +
			"Paths are resolved relative to the config file location.",
		"ignore_keys": "Suppress diff entries whose dot-separated path contains any of these key segments " +
			"(exact segment match at any depth). Merged with global_ignore_keys.",
		"ignore_patterns": "Environment/marker tokens to strip before comparing values. " +
			"A changed entry is suppressed when both values, after stripping these tokens, are identical. " +
			"Accepts a list or a comma-separated string (e.g. 'qa,prod,stage'). Merged with global_ignore_patterns.",
		"query": "Filter diff entries for this pair only. " +
			"Bare prefix: 'configmap.data' keeps only paths under that key. " +
			"JMESPath expression: '[?kind==\"changed\"]' for arbitrary filtering. " +
			"Overrides the top-level query for this pair.",
	})
}

// JSONSchemaExtend documents the Config fields and attaches a usage example.
func (Config) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"format": "Output format: 'table' (default, rich table), 'unified' (git-diff style), or 'json' (machine-readable). " +
			"Overridden by --format / -o / --unified on the command line.",
		"query": "Global filter applied to every diff pair. " +
			"Bare prefix: 'configmap.data' keeps only paths under that key. " +
			"JMESPath expression: '[?kind==\"changed\"]' for arbitrary filtering. " +
			"Overridden by -q on the command line; per-pair 'query' takes precedence.",
		"global_ignore_keys": "Key segments to suppress across all diff pairs. " +
			"A path is suppressed if any segment matches at any depth " +
			"(e.g. 'metadata' suppresses 'spec.metadata.name').",
		"global_ignore_patterns": "Environment/marker tokens stripped from values before comparing. " +
			"A changed entry is suppressed when both values are identical after stripping. " +
			"Accepts a list or a comma-separated string (e.g. 'qa,prod,stage'). " +
			"Overridden by --ignore-pattern on the command line.",
		"diffs": "List of diff entries. Each entry compares two or more files. " +
			"When 3+ files are given, all N-choose-2 pairs are compared automatically.",
	})

	if p, ok := s.Properties.Get("format"); ok {
		p.Enum = []any{string(FormatUnified), string(FormatTable), string(FormatJSON)}
		p.Default = string(DefaultFormat)
	}

	s.Extras = map[string]any{
		"example": map[string]any{
			"format":                 "table",
			"query":                  "configmap.data",
			"global_ignore_keys":     []string{"metadata", "status"},
			"global_ignore_patterns": "qa,prod,stage",
			"diffs": []map[string]any{
				{
					"files":       []string{"helm/admin/values-qa.yaml", "helm/admin/values-prod.yaml"},
					"ignore_keys": []string{"timestamp"},
					"query":       "configmap.data",
				},
				{
					"files": []string{
						"helm/app/values-dev.yaml",
						"helm/app/values-stage.yaml",
						"helm/app/values-prod.yaml",
					},
				},
			},
		},
	}
}

func describe(s *jsonschema.Schema, descriptions map[string]string) {
	if s.Properties == nil {
		return
	}
	for name, text := range descriptions {
		if p, ok := s.Properties.Get(name); ok {
			p.Description = text
		}
	}
}

// SchemaYAML renders the cu_diff.yml JSON schema as YAML, for humans and for
// agents generating config files.
func SchemaYAML() ([]byte, error) {
	reflector := &jsonschema.Reflector{ExpandedStruct: true, DoNotReference: true}
	schema := reflector.Reflect(&Config{})
	schema.Description = "Configuration file format for `cu diff --config`."

	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return yaml.Marshal(generic)
}
