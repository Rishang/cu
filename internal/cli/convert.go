package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	yaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Rishang/cloudutil/internal/ui"
)

// newConvertCommand builds one of the two format converters. Both read a file
// or stdin and write to stdout, so only the conversion itself differs.
func newConvertCommand(use, short, example string, convert func([]byte) error) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			data, err := readInput(file)
			if err != nil {
				return err
			}
			return convert(data)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Read from this file instead of stdin.")
	return cmd
}

func newJSON2YAMLCommand() *cobra.Command {
	return newConvertCommand(
		"json2yaml",
		"Convert JSON to YAML",
		"  cat file.json | cu json2yaml\n  cu json2yaml -f file.json",
		func(data []byte) error {
			out, err := yaml.JSONToYAML(data)
			if err != nil {
				return fmt.Errorf("invalid JSON: %w", err)
			}
			_, err = ui.Out.Write(out)
			return err
		},
	)
}

func newYAML2JSONCommand() *cobra.Command {
	return newConvertCommand(
		"yaml2json",
		"Convert YAML to JSON",
		"  cat file.yml | cu yaml2json\n  cu yaml2json -f file.yaml",
		yamlToJSON,
	)
}

// yamlToJSON prints data as JSON, one line per YAML document. yaml.YAMLToJSON
// converts only the first document, and dropping the rest of a '---' separated
// k8s manifest would be silent data loss, so the documents are split first.
// JSON has no multi-document form; the de facto one is JSON Lines, which keeps
// each document independently parseable and pipeable into jq.
func yamlToJSON(data []byte) error {
	file, err := parser.ParseBytes(data, 0)
	if err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	for _, doc := range file.Docs {
		converted, err := yaml.YAMLToJSON([]byte(doc.String()))
		if err != nil {
			return fmt.Errorf("invalid YAML: %w", err)
		}
		if err := ui.PrintJSONBytes(converted); err != nil {
			return err
		}
	}
	return nil
}

// readInput returns the bytes to convert, from path when given and stdin
// otherwise. A terminal on stdin means nothing was piped in, which would
// otherwise look like a hang.
func readInput(path string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("no input: pipe data on stdin or pass -f <file>")
	}
	return io.ReadAll(os.Stdin)
}
