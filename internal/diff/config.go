package diff

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	yaml "github.com/goccy/go-yaml"
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
	Files          []string `yaml:"files"`
	IgnoreKeys     []string `yaml:"ignore_keys"`
	IgnorePatterns Patterns `yaml:"ignore_patterns"`
	Query          string   `yaml:"query"`
}

// Config is the cu_diff.yml file format.
type Config struct {
	Format               Format   `yaml:"format"`
	Query                string   `yaml:"query"`
	GlobalIgnoreKeys     []string `yaml:"global_ignore_keys"`
	GlobalIgnorePatterns Patterns `yaml:"global_ignore_patterns"`
	Diffs                []Diff   `yaml:"diffs"`
}

// LoadConfig reads and validates a cu_diff.yml file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, err
	}

	cfg := &Config{}
	// Strict: a typo like `ignore_key:` would otherwise parse fine and silently
	// suppress nothing, which is exactly the failure the config exists to avoid.
	// The schema below already promises additionalProperties: false.
	// An empty file decodes to io.EOF; let Validate give it the better message.
	if err := yaml.NewDecoder(bytes.NewReader(data), yaml.DisallowUnknownField()).Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
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

// schemaDoc is the cu_diff.yml schema, served by --print-schema so that humans
// and agents can generate a valid config. It is written out rather than
// reflected off the structs: the document never varies per run, and reflecting
// it cost a dependency plus three of its own. TestSchemaCoversEveryField fails
// if a field is added to Config or Diff without being documented here.
const schemaDoc = `$schema: https://json-schema.org/draft/2020-12/schema
description: Configuration file format for ` + "`cu diff --config`" + `.
type: object
additionalProperties: false
required:
  - diffs
properties:
  format:
    type: string
    enum: [unified, table, json]
    default: table
    description: >-
      Output format: 'table' (default, rich table), 'unified' (git-diff style),
      or 'json' (machine-readable). Overridden by --format / -o / --unified on
      the command line.
  query:
    type: string
    description: >-
      Global filter applied to every diff pair. Bare prefix: 'configmap.data'
      keeps only paths under that key. JMESPath expression: '[?kind=="changed"]'
      for arbitrary filtering. Overridden by -q on the command line; per-pair
      'query' takes precedence.
  global_ignore_keys:
    type: array
    items: {type: string}
    description: >-
      Key segments to suppress across all diff pairs. A path is suppressed if
      any segment matches at any depth (e.g. 'metadata' suppresses
      'spec.metadata.name').
  global_ignore_patterns:
    type: array
    items: {type: string}
    description: >-
      Environment/marker tokens stripped from values before comparing. A changed
      entry is suppressed when both values are identical after stripping.
      Accepts a list or a comma-separated string (e.g. 'qa,prod,stage').
      Overridden by --ignore-pattern on the command line.
  diffs:
    type: array
    description: >-
      List of diff entries. Each entry compares two or more files. When 3+ files
      are given, all N-choose-2 pairs are compared automatically.
    items:
      type: object
      additionalProperties: false
      required:
        - files
      properties:
        files:
          type: array
          items: {type: string}
          description: >-
            Two or more file paths to compare (JSON, YAML, TOML, or HCL). When
            3+ files are given, all N-choose-2 pairs are compared. Paths are
            resolved relative to the config file location.
        ignore_keys:
          type: array
          items: {type: string}
          description: >-
            Suppress diff entries whose dot-separated path contains any of these
            key segments (exact segment match at any depth). Merged with
            global_ignore_keys.
        ignore_patterns:
          type: array
          items: {type: string}
          description: >-
            Environment/marker tokens to strip before comparing values. A
            changed entry is suppressed when both values, after stripping these
            tokens, are identical. Accepts a list or a comma-separated string
            (e.g. 'qa,prod,stage'). Merged with global_ignore_patterns.
        query:
          type: string
          description: >-
            Filter diff entries for this pair only. Bare prefix:
            'configmap.data' keeps only paths under that key. JMESPath
            expression: '[?kind=="changed"]' for arbitrary filtering. Overrides
            the top-level query for this pair.
example:
  format: table
  query: configmap.data
  global_ignore_keys: [metadata, status]
  global_ignore_patterns: qa,prod,stage
  diffs:
    - files: [helm/admin/values-qa.yaml, helm/admin/values-prod.yaml]
      ignore_keys: [timestamp]
      query: configmap.data
    - files:
        - helm/app/values-dev.yaml
        - helm/app/values-stage.yaml
        - helm/app/values-prod.yaml
`

// SchemaYAML returns the cu_diff.yml schema as YAML.
func SchemaYAML() []byte {
	return []byte(schemaDoc)
}
