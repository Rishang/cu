package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	noColor        bool
	query          string
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
  --format json              machine-readable JSON, for piping into jq
  -q spec.replicas           show only diffs under a path prefix`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDiff(f)
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
	flags.BoolVar(&f.noColor, "no-color", false, "Disable colored output.")
	flags.StringVarP(&f.query, "query", "q", "",
		"Show only diffs at or under this path prefix, e.g. -q spec.replicas.")

	return cmd
}

// exitTrouble is cu diff's code for "could not compare" (bad flags, a missing
// or unparsable file), kept apart from 1 = "differences found" the way diff(1)
// does, so CI can tell drift from a broken pipeline.
const exitTrouble = 2

func runDiff(f *diffFlags) error {
	// The diff itself is styled onto stdout, so `> file` or `| grep` must not
	// get escape codes just because stderr is still a terminal.
	if f.noColor || !ui.OutIsTerminal() {
		ui.SetColor(false)
	}

	if len(f.files) > 0 && f.config != "" {
		ui.Error("Use either -f flags or a config file argument, not both.")
		return exitWith(exitTrouble)
	}

	if len(f.files) == 0 && f.config == "" {
		if _, err := os.Stat(defaultConfigName); err == nil {
			f.config = defaultConfigName
		} else {
			ui.Error("Specify -f <file> -f <file> or --config <%s>.", defaultConfigName)
			return exitWith(exitTrouble)
		}
	}

	// --unified beats --format; both fall back to the config file, then table.
	cliFormat := diff.Format(f.format)
	if f.unified {
		cliFormat = diff.FormatUnified
	}
	if cliFormat != "" && !diff.ValidFormat(cliFormat) {
		ui.Error("Invalid format %q: expected unified, table, or json.", f.format)
		return exitWith(exitTrouble)
	}

	if len(f.files) > 0 {
		return runInlineDiff(f, cliFormat)
	}
	return runConfigDiff(f, cliFormat)
}

func runInlineDiff(f *diffFlags, cliFormat diff.Format) error {
	if len(f.files) < 2 {
		ui.Error("At least 2 -f/--file values required, got %d.", len(f.files))
		return exitWith(exitTrouble)
	}
	if cliFormat == "" {
		cliFormat = diff.DefaultFormat
	}

	counts, pairs, err := diffPairs(f.files, f.files, "", diff.FilterRules{
		LocalIgnoreKeys:     f.ignoreKeys,
		LocalIgnorePatterns: f.ignorePatterns,
	}, cliFormat, f.query)
	if err != nil {
		return err
	}
	return finish(counts, pairs, cliFormat)
}

func runConfigDiff(f *diffFlags, cliFormat diff.Format) error {
	cfg, err := diff.LoadConfig(f.config)
	if err != nil {
		ui.Error("%v", err)
		return exitWith(exitTrouble)
	}

	format := cliFormat
	if format == "" {
		format = cfg.Format
	}

	configDir, err := filepath.Abs(filepath.Dir(f.config))
	if err != nil {
		return err
	}

	var counts tally
	pairs := 0
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

		c, n, err := diffPairs(resolved, entry.Files,
			fmt.Sprintf("DIFF %d/%d", i+1, len(cfg.Diffs)), diff.FilterRules{
				GlobalIgnoreKeys:     cfg.GlobalIgnoreKeys,
				LocalIgnoreKeys:      entry.IgnoreKeys,
				GlobalIgnorePatterns: cfg.GlobalIgnorePatterns,
				LocalIgnorePatterns:  entry.IgnorePatterns,
			}, format, query)
		if err != nil {
			return err
		}
		counts.add(c)
		pairs += n
	}
	return finish(counts, pairs, format)
}

// diffPairs renders every 2-combination of the given files. paths are what gets
// loaded, display what the header shows (config mode keeps the paths as written).
// prefix labels the group and is empty in inline mode. It returns the number of
// differences found and the number of pairs compared.
func diffPairs(paths, display []string, prefix string, rules diff.FilterRules, format diff.Format, query string) (counts tally, pairs int, err error) {
	w := decorationWriter(format)
	combos := indexPairs(len(paths))
	for i, pair := range combos {
		label := prefix
		if label == "" {
			label = "DIFF"
		}
		if len(combos) > 1 {
			label = strings.TrimSpace(fmt.Sprintf("%s PAIR %d/%d", prefix, i+1, len(combos)))
		}
		fileA, fileB := paths[pair[0]], paths[pair[1]]
		printPairHeader(w, label, fileA, fileB, display[pair[0]], display[pair[1]])

		c, err := executeDiff(fileA, fileB, rules, format, query)
		if err != nil {
			return tally{}, 0, err
		}
		counts.add(c)
		fmt.Fprintln(w)
	}
	return counts, len(combos), nil
}

// decorationWriter is where the headers and summary around each diff go. For
// unified/table they are part of what a human pipes into grep, so stdout; for
// JSON they would break `| jq`, so stderr keeps stdout a pure JSON stream.
func decorationWriter(format diff.Format) io.Writer {
	if format == diff.FormatJSON {
		return ui.Err
	}
	return ui.Out
}

// finish prints the cross-pair summary and exits 1 when anything differed.
func finish(counts tally, pairs int, format diff.Format) error {
	if pairs > 1 {
		printOverallSummary(decorationWriter(format), counts, pairs)
	}
	if counts.total() > 0 {
		return exitWith(1)
	}
	return nil
}

// tally counts surviving differences by kind, for the cross-pair summary.
// A type change counts as changed, as it does in the per-pair summary.
type tally struct{ added, removed, changed int }

func (t tally) total() int { return t.added + t.removed + t.changed }

func (t *tally) add(o tally) {
	t.added += o.added
	t.removed += o.removed
	t.changed += o.changed
}

// executeDiff loads, diffs, filters and renders one pair, returning the
// differences that survived filtering, by kind.
func executeDiff(fileA, fileB string, rules diff.FilterRules, format diff.Format, query string) (tally, error) {
	dataA, err := diff.LoadFile(fileA)
	if err != nil {
		ui.Error("%v", err)
		return tally{}, exitWith(exitTrouble)
	}
	dataB, err := diff.LoadFile(fileB)
	if err != nil {
		ui.Error("%v", err)
		return tally{}, exitWith(exitTrouble)
	}

	kept, ignored := diff.Apply(diff.Compute(dataA, dataB), rules)

	// Only what is rendered as a diff is filtered; the ignored list is a dim
	// footer saying what the rules suppressed, not queryable output.
	if query != "" {
		kept = diff.Query(kept, query)
	}

	diff.Render(kept, diff.RenderOptions{
		Format:  format,
		FileA:   filepath.Base(fileA),
		FileB:   filepath.Base(fileB),
		BranchA: diff.GitBranch(fileA),
		BranchB: diff.GitBranch(fileB),
		Ignored: ignored,
	})

	var counts tally
	for _, d := range kept {
		switch d.Kind {
		case diff.KindAdded:
			counts.added++
		case diff.KindRemoved:
			counts.removed++
		default:
			counts.changed++
		}
	}
	return counts, nil
}

func printPairHeader(w io.Writer, label, fileA, fileB, displayA, displayB string) {
	tag := func(path string) string {
		if branch := diff.GitBranch(path); branch != "" {
			return ui.Dim.Sprintf("  (%s)", branch)
		}
		return ""
	}
	ui.Rule(w, ui.Bold.Render(label), ui.Cyan)
	fmt.Fprintf(w, "  %s  %s%s\n", ui.Red.Render("−"), ui.Cyan.Render(displayA), tag(fileA))
	fmt.Fprintf(w, "  %s  %s%s\n", ui.Green.Render("+"), ui.Cyan.Render(displayB), tag(fileB))
	fmt.Fprintln(w)
}

func printOverallSummary(w io.Writer, counts tally, pairs int) {
	if counts.total() == 0 {
		ui.Rule(w, ui.BoldGreen.Render("✅  ALL DIFFS PASSED — no differences detected."), ui.Green)
		return
	}
	noun := "changes"
	if counts.total() == 1 {
		noun = "change"
	}
	ui.Rule(w, ui.BoldRed.Sprintf("❌  %d %s, %d added, %d removed across %d pair(s)",
		counts.total(), noun, counts.added, counts.removed, pairs), ui.Red)
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
