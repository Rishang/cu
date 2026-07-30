package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rishang/cloudutil/internal/diff"
	"github.com/Rishang/cloudutil/internal/ui"
)

// defaultConfigName is picked up when neither -f nor --config is given.
const defaultConfigName = "cu_diff.yml"

type diffFlags struct {
	config         string
	files          []string
	ignoreKeys     []string
	ignorePatterns []string
	format         string
	unified        bool
	color          bool
	noColor        bool
	query          string
	printSchema    bool
}

func newDiffCommand() *cobra.Command {
	f := &diffFlags{}

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Semantic diff for structured config files (JSON, YAML, TOML, HCL)",
		Long: `Semantic diff — compare JSON, YAML, TOML or HCL config files structurally.

Compare two files inline:
  cu diff -f prod.yaml -f stage.yaml

Compare using a config file (global ignore rules and multiple pairs):
  cu diff --config cu_diff.yml

Common flags:
  --unified / -u             git-diff style output instead of table
  --ignore-key metadata      suppress paths containing 'metadata'
  --ignore-pattern dev       suppress values differing only by 'dev'
  --format json              machine-readable JSON
  -q spec.replicas           show only diffs under a path prefix
  -q "[?kind=='changed']"    JMESPath filter on diff entries`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiff(cmd, f)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&f.config, "config", "c", "", "Diff config YAML file.")
	flags.StringArrayVarP(&f.files, "file", "f", nil,
		"File to compare (repeat twice: -f a.yaml -f b.yaml).")
	flags.StringArrayVar(&f.ignoreKeys, "ignore-key", nil,
		"Suppress paths containing this key segment (repeatable).")
	flags.StringArrayVar(&f.ignorePatterns, "ignore-pattern", nil,
		"Suppress diffs whose values differ only by this token (repeatable).")
	flags.StringVarP(&f.format, "format", "o", "",
		"Output format: unified, table, or json.")
	flags.BoolVarP(&f.unified, "unified", "u", false,
		"Shorthand for --format unified (git-diff style).")
	flags.BoolVar(&f.color, "color", true, "Enable colored output.")
	flags.BoolVar(&f.noColor, "no-color", false, "Disable colored output.")
	flags.StringVarP(&f.query, "query", "q", "",
		"Filter diff entries by path prefix or JMESPath expression, e.g. -q \"[?kind=='changed']\".")
	flags.BoolVar(&f.printSchema, "print-schema", false,
		"Print the cu_diff.yml JSON schema as YAML and exit. Useful for agents "+
			"generating config files: pipe it to an LLM with your file list.")

	return cmd
}

func runDiff(cmd *cobra.Command, f *diffFlags) error {
	if f.printSchema {
		schema, err := diff.SchemaYAML()
		if err != nil {
			return err
		}
		fmt.Fprint(ui.Out, string(schema))
		return nil
	}

	if f.noColor || !f.color {
		ui.SetColor(false)
	} else if cmd.Flags().Changed("color") {
		ui.SetColor(true)
	}

	if len(f.files) > 0 && f.config != "" {
		ui.Error("Use either -f flags or a config file argument, not both.")
		return exitWith(1)
	}

	if len(f.files) == 0 && f.config == "" {
		if _, err := os.Stat(defaultConfigName); err == nil {
			f.config = defaultConfigName
		} else {
			ui.Error("Specify -f <file> -f <file> or --config <%s>.", defaultConfigName)
			return exitWith(1)
		}
	}

	// --unified beats --format; both fall back to the config file, then table.
	cliFormat := diff.Format(f.format)
	if f.unified {
		cliFormat = diff.FormatUnified
	}
	if cliFormat != "" && !diff.ValidFormat(cliFormat) {
		ui.Error("Invalid format %q: expected unified, table, or json.", f.format)
		return exitWith(1)
	}

	if len(f.files) > 0 {
		return runInlineDiff(f, cliFormat)
	}
	return runConfigDiff(f, cliFormat)
}

func runInlineDiff(f *diffFlags, cliFormat diff.Format) error {
	if len(f.files) < 2 {
		ui.Error("At least 2 -f/--file values required, got %d.", len(f.files))
		return exitWith(1)
	}
	if cliFormat == "" {
		cliFormat = diff.DefaultFormat
	}

	pairs := indexPairs(len(f.files))
	total := 0
	for i, pair := range pairs {
		label := "DIFF"
		if len(pairs) > 1 {
			label = fmt.Sprintf("PAIR %d/%d", i+1, len(pairs))
		}
		fileA, fileB := f.files[pair[0]], f.files[pair[1]]
		printPairHeader(label, fileA, fileB, fileA, fileB)

		count, err := executeDiff(fileA, fileB, diff.FilterRules{
			LocalIgnoreKeys:     f.ignoreKeys,
			LocalIgnorePatterns: f.ignorePatterns,
		}, cliFormat, f.query)
		if err != nil {
			return err
		}
		total += count
		ui.Print("")
	}
	return finish(total, len(pairs))
}

func runConfigDiff(f *diffFlags, cliFormat diff.Format) error {
	cfg, err := diff.LoadConfig(f.config)
	if err != nil {
		ui.Error("%v", err)
		return exitWith(1)
	}

	format := cliFormat
	if format == "" {
		format = cfg.Format
	}

	configDir, err := filepath.Abs(filepath.Dir(f.config))
	if err != nil {
		return err
	}

	total := 0
	for i, entry := range cfg.Diffs {
		// Query precedence: CLI -q, then the per-pair query, then the global one.
		query := f.query
		if query == "" {
			query = entry.Query
		}
		if query == "" {
			query = cfg.Query
		}

		// Relative paths resolve against the config file; absolute ones stand.
		resolved := make([]string, len(entry.Files))
		for j, file := range entry.Files {
			if filepath.IsAbs(file) {
				resolved[j] = file
				continue
			}
			resolved[j] = filepath.Join(configDir, file)
		}

		pairs := indexPairs(len(resolved))
		for j, pair := range pairs {
			label := fmt.Sprintf("DIFF %d/%d", i+1, len(cfg.Diffs))
			if len(pairs) > 1 {
				label += fmt.Sprintf(" PAIR %d/%d", j+1, len(pairs))
			}
			fileA, fileB := resolved[pair[0]], resolved[pair[1]]
			printPairHeader(label, fileA, fileB, entry.Files[pair[0]], entry.Files[pair[1]])

			count, err := executeDiff(fileA, fileB, diff.FilterRules{
				GlobalIgnoreKeys:     cfg.GlobalIgnoreKeys,
				LocalIgnoreKeys:      entry.IgnoreKeys,
				GlobalIgnorePatterns: cfg.GlobalIgnorePatterns,
				LocalIgnorePatterns:  entry.IgnorePatterns,
			}, format, query)
			if err != nil {
				return err
			}
			total += count
			ui.Print("")
		}
	}
	return finish(total, len(cfg.Diffs))
}

// finish prints the cross-group summary and exits 1 when anything differed.
func finish(total, groups int) error {
	if groups > 1 {
		printOverallSummary(total, groups)
	}
	if total > 0 {
		return exitWith(1)
	}
	return nil
}

// executeDiff loads, diffs, filters and renders one pair, returning how many
// differences survived filtering.
func executeDiff(fileA, fileB string, rules diff.FilterRules, format diff.Format, query string) (int, error) {
	dataA, err := diff.LoadFile(fileA)
	if err != nil {
		ui.Error("%v", err)
		return 0, exitWith(1)
	}
	dataB, err := diff.LoadFile(fileB)
	if err != nil {
		ui.Error("%v", err)
		return 0, exitWith(1)
	}

	kept, ignored := diff.Apply(diff.Compute(dataA, dataB), rules)

	if query != "" {
		if kept, err = diff.Query(kept, query); err != nil {
			ui.Error("%v", err)
			return 0, exitWith(1)
		}
		if ignored, err = diff.Query(ignored, query); err != nil {
			ui.Error("%v", err)
			return 0, exitWith(1)
		}
	}

	diff.Render(kept, diff.RenderOptions{
		Format:  format,
		FileA:   filepath.Base(fileA),
		FileB:   filepath.Base(fileB),
		BranchA: diff.GitBranch(fileA),
		BranchB: diff.GitBranch(fileB),
		HCL:     diff.IsHCL(fileA) && diff.IsHCL(fileB),
		Ignored: ignored,
	})
	return len(kept), nil
}

func printPairHeader(label, fileA, fileB, displayA, displayB string) {
	tag := func(path string) string {
		if branch := diff.GitBranch(path); branch != "" {
			return ui.Dim.Sprintf("  (%s)", branch)
		}
		return ""
	}
	ui.Rule(ui.Bold.Render(label), ui.Cyan)
	ui.Printf("  %s  %s%s", ui.Red.Render("−"), ui.Cyan.Render(displayA), tag(fileA))
	ui.Printf("  %s  %s%s", ui.Green.Render("+"), ui.Cyan.Render(displayB), tag(fileB))
	ui.Print("")
}

func printOverallSummary(total, groups int) {
	if total == 0 {
		ui.Rule(ui.BoldGreen.Render("✅  ALL DIFFS PASSED — no differences detected."), ui.Green)
		return
	}
	ui.Rule(ui.BoldRed.Sprintf("❌  %d difference(s) across %d pair(s).", total, groups), ui.Red)
}

// indexPairs returns every 2-combination of indices, in order.
func indexPairs(n int) [][2]int {
	var pairs [][2]int
	for i := range n {
		for j := i + 1; j < n; j++ {
			pairs = append(pairs, [2]int{i, j})
		}
	}
	return pairs
}
