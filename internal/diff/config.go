package diff

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

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

// Diff is one comparison entry: two or more files plus its local ignore rules.
type Diff struct {
	Files          []string `yaml:"files"`
	IgnoreKeys     []string `yaml:"ignore_keys"`
	IgnorePatterns []string `yaml:"ignore_patterns"`
	Query          string   `yaml:"query"`
}

// Config is the cu_diff.yml file format.
type Config struct {
	Format               Format   `yaml:"format"`
	Query                string   `yaml:"query"`
	GlobalIgnoreKeys     []string `yaml:"global_ignore_keys"`
	GlobalIgnorePatterns []string `yaml:"global_ignore_patterns"`
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
	// An empty file decodes to io.EOF; let validate give it the better message.
	if err := yaml.NewDecoder(bytes.NewReader(data), yaml.DisallowUnknownField()).Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if cfg.Format == "" {
		cfg.Format = DefaultFormat
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate rejects a config that would silently do nothing useful.
func (c *Config) validate() error {
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
