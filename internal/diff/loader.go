package diff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	yaml "github.com/goccy/go-yaml"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/tmccombs/hcl2json/convert"
)

// supportedExtensions lists every file type cu diff can parse.
var supportedExtensions = []string{".hcl", ".json", ".tf", ".tfvars", ".toml", ".yaml", ".yml"}

// LoadFile parses a structured config file into plain Go values.
func LoadFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("File not found: %s", path)
		}
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(path))
	if !slices.Contains(supportedExtensions, ext) {
		return nil, fmt.Errorf("Unsupported format %q. Supported: %s",
			ext, strings.Join(supportedExtensions, ", "))
	}

	switch ext {
	case ".json":
		v, err := decodeJSON(data)
		if err != nil {
			return nil, fmt.Errorf("Invalid JSON in %q: %w", path, err)
		}
		return v, nil
	case ".toml":
		var v any
		if err := toml.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("Invalid TOML in %q: %w", path, err)
		}
		return v, nil
	case ".tf", ".hcl", ".tfvars":
		converted, err := convert.Bytes(data, path, convert.Options{})
		if err != nil {
			return nil, fmt.Errorf("Invalid HCL in %q: %w", path, err)
		}
		v, err := decodeJSON(converted)
		if err != nil {
			return nil, fmt.Errorf("Invalid HCL in %q: %w", path, err)
		}
		return v, nil
	default: // .yaml / .yml
		v, err := decodeYAML(data)
		if err != nil {
			return nil, fmt.Errorf("Invalid YAML in %q: %w", path, err)
		}
		return v, nil
	}
}

// decodeJSON keeps numeric literals intact so 42 stays an int and 2.0 a float.
func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// Anything after the first value would be dropped, and a dropped half of a
	// file reads as "no differences".
	if dec.More() {
		return nil, errors.New("unexpected data after the top-level value")
	}
	return v, nil
}

// decodeYAML keeps every document of a multi-document file: k8s manifests are
// routinely '---' separated, and decoding only the first silently hides every
// difference in the rest. One document stays a bare value; several become a
// list, which the engine then compares document by document.
func decodeYAML(data []byte) (any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []any
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		docs = append(docs, v)
	}
	if len(docs) <= 1 { // an empty file is nil, not an empty list
		if len(docs) == 0 {
			return nil, nil
		}
		return docs[0], nil
	}
	return docs, nil
}

// branchCache keys resolved branches by directory: header and renderer both ask
// for the same two files, and N-way diffs ask again per pair.
//
// ponytail: unsynchronized — diffs run on one goroutine. Add a mutex if that changes.
var branchCache = map[string]string{}

// GitBranch returns the branch for the repo containing path, or "" when there
// is none (not a repo, detached HEAD, or git unavailable). The branch is only
// ever decoration on a diff header, so every failure is the same empty answer.
func GitBranch(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(abs)
	if branch, ok := branchCache[dir]; ok {
		return branch
	}

	branch := ""
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	if out, err := cmd.Output(); err == nil {
		if name := strings.TrimSpace(string(out)); name != "HEAD" { // "HEAD" means detached
			branch = name
		}
	}
	branchCache[dir] = branch
	return branch
}
