package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Rishang/cloudutil/internal/ui"
)

// RenderOptions describes one rendered diff pair.
type RenderOptions struct {
	Format  Format
	FileA   string
	FileB   string
	BranchA string
	BranchB string
	HCL     bool // format values as HCL literals
	Ignored []Entry
}

// valueFormatter renders a value for display.
type valueFormatter func(any) string

// Render writes entries in the requested format. Table and unified output go to
// stderr; JSON goes to stdout so it stays pipeable.
func Render(entries []Entry, o RenderOptions) {
	format := formatValue
	if o.HCL {
		format = formatHCL
	}
	switch o.Format {
	case FormatJSON:
		renderJSON(entries, o)
	case FormatTable:
		renderTable(entries, o, format)
	default:
		renderUnified(entries, o, format)
	}
}

// ── unified ───────────────────────────────────────────────────────────────────

type renderLine struct {
	sym      string
	symStyle ui.Style
	key      string
	keyStyle ui.Style
	segs     []ui.Segment
}

func renderUnified(entries []Entry, o RenderOptions, format valueFormatter) {
	printUnifiedHeader(o)

	if len(entries) == 0 {
		ui.Print("")
		ui.Print(ui.BoldGreen.Render("✓  No differences"))
		renderIgnored(o.Ignored)
		return
	}

	groups := map[string][]Entry{}
	for _, e := range sortByPath(entries) {
		section := "(root)"
		if len(e.Path) > 0 {
			section = fmt.Sprint(e.Path[0])
		}
		groups[section] = append(groups[section], e)
	}

	var added, removed, changed int
	for _, section := range slices.Sorted(maps.Keys(groups)) {
		lines := buildLines(groups[section], section, format)
		label := fmt.Sprintf("(%d change%s)", len(lines), plural(len(lines)))
		ui.Print("")
		ui.Printf("%s  %s", ui.BoldCyan.Sprintf("@@ %s @@", section), ui.Dim.Render(label))

		pad := 0
		for _, ln := range lines {
			if w := ui.TextWidth(ln.key); ln.key != "" && w > pad {
				pad = w
			}
		}
		for _, ln := range lines {
			switch ln.sym {
			case "+":
				added++
			case "-":
				removed++
			default:
				changed++
			}
			printLine(ln, pad)
		}
	}

	renderIgnored(o.Ignored)
	ui.Print("")
	renderSummary(added, removed, changed)
}

func printUnifiedHeader(o RenderOptions) {
	tag := func(branch string) string {
		if branch == "" {
			return ""
		}
		return ui.Dim.Sprintf("  (%s)", branch)
	}
	ui.Print(ui.BoldRed.Sprintf("--- a/%s", o.FileA) + tag(o.BranchA))
	ui.Print(ui.BoldGreen.Sprintf("+++ b/%s", o.FileB) + tag(o.BranchB))
}

func printLine(ln renderLine, pad int) {
	var b strings.Builder
	b.WriteString(ln.symStyle.Render(ln.sym))
	b.WriteString("  ")
	if ln.key != "" {
		key := ln.key + ":"
		if gap := pad + 1 - ui.TextWidth(key); gap > 0 {
			key += strings.Repeat(" ", gap)
		}
		b.WriteString(ln.keyStyle.Render(key))
		b.WriteString("  ")
	}
	for _, seg := range ln.segs {
		b.WriteString(seg.Style.Render(seg.Text))
	}
	ui.Print(b.String())
}

func buildLines(group []Entry, section string, format valueFormatter) []renderLine {
	var lines []renderLine
	for _, e := range group {
		rel := relativeKey(e.PathStr(), section)
		switch e.Kind {
		case KindAdded:
			for _, kv := range expand(rel, e.New) {
				lines = append(lines, renderLine{
					sym: "+", symStyle: ui.Green, key: kv.key, keyStyle: ui.Green,
					segs: []ui.Segment{{Text: format(kv.value), Style: ui.Green}},
				})
			}
		case KindRemoved:
			for _, kv := range expand(rel, e.Old) {
				lines = append(lines, renderLine{
					sym: "-", symStyle: ui.Red, key: kv.key, keyStyle: ui.Red,
					segs: []ui.Segment{{Text: format(kv.value), Style: ui.Red}},
				})
			}
		case KindChanged:
			lines = append(lines, changedLines(rel, e.Old, e.New, "", format)...)
		case KindTypeChanged:
			note := fmt.Sprintf("  (%s → %s)", typeName(e.Old), typeName(e.New))
			lines = append(lines, changedLines(rel, e.Old, e.New, note, format)...)
		}
	}
	return lines
}

func changedLines(key string, old, new any, note string, format valueFormatter) []renderLine {
	if isScalar(old) && isScalar(new) {
		segs := []ui.Segment{
			{Text: format(old), Style: ui.Red},
			{Text: " → ", Style: ui.Dim},
			{Text: format(new), Style: ui.Green},
		}
		if note != "" {
			segs = append(segs, ui.Segment{Text: note, Style: ui.DimItalic})
		}
		return []renderLine{{sym: "~", symStyle: ui.Yellow, key: key, keyStyle: ui.Bold, segs: segs}}
	}

	// Non-scalar values are too long to show inline; emit removed then added,
	// with any type note attached to the removed side.
	removedSegs := []ui.Segment{{Text: format(old), Style: ui.Red}}
	if note != "" {
		removedSegs = append(removedSegs, ui.Segment{Text: note, Style: ui.DimItalic})
	}
	return []renderLine{
		{sym: "-", symStyle: ui.Red, key: key, keyStyle: ui.Red, segs: removedSegs},
		{sym: "+", symStyle: ui.Green, key: key, keyStyle: ui.Green,
			segs: []ui.Segment{{Text: format(new), Style: ui.Green}}},
	}
}

// relativeKey strips the top-level section prefix from a dotted path.
func relativeKey(path, section string) string {
	switch {
	case path == section:
		return ""
	case strings.HasPrefix(path, section+"."):
		return path[len(section)+1:]
	case strings.HasPrefix(path, section+"["):
		return path[len(section):]
	default:
		return path
	}
}

type keyValue struct {
	key   string
	value any
}

// expand flattens an added or removed value into leaf key/value pairs, so a
// whole new block prints one line per leaf rather than as a JSON blob.
func expand(prefix string, value any) []keyValue {
	switch t := value.(type) {
	case map[string]any:
		var pairs []keyValue
		for _, k := range slices.Sorted(maps.Keys(t)) {
			child := k
			if prefix != "" {
				child = prefix + "." + k
			}
			pairs = append(pairs, expand(child, t[k])...)
		}
		if len(pairs) == 0 {
			return []keyValue{{prefix, value}}
		}
		return pairs
	case []any:
		var pairs []keyValue
		for i, v := range t {
			pairs = append(pairs, expand(fmt.Sprintf("%s[%d]", prefix, i), v)...)
		}
		if len(pairs) == 0 {
			return []keyValue{{prefix, value}}
		}
		return pairs
	default:
		return []keyValue{{prefix, value}}
	}
}

// ── table ─────────────────────────────────────────────────────────────────────

func renderTable(entries []Entry, o RenderOptions, format valueFormatter) {
	if len(entries) == 0 {
		renderIgnored(o.Ignored)
		ui.Print(ui.BoldGreen.Render("✓  No differences"))
		return
	}

	columnLabel := func(prefix, name, branch string, style ui.Style) ui.Cell {
		cell := ui.Cell{
			{Text: prefix, Style: style},
			{Text: " " + name, Style: ui.Bold},
		}
		if branch != "" {
			cell = append(cell, ui.Segment{Text: " (" + branch + ")", Style: ui.Dim})
		}
		return cell
	}

	table := &ui.Table{
		Headers: []ui.Cell{
			ui.Text("", ui.Plain),
			ui.Text("Path", ui.BoldCyan),
			columnLabel("−", o.FileA, o.BranchA, ui.BoldRed),
			columnLabel("+", o.FileB, o.BranchB, ui.BoldGreen),
		},
		Aligns: []ui.Align{ui.AlignCenter, ui.AlignLeft, ui.AlignLeft, ui.AlignLeft},
		// Paths stay intact; the value columns give up width instead.
		NoShrink: []bool{true, true, false, false},
		Border:   ui.Dim,
	}

	dash := ui.Text("—", ui.Dim)
	var added, removed, changed int
	for _, e := range sortByPath(entries) {
		var oldCell, newCell ui.Cell
		sym, symStyle := "~", ui.Yellow
		switch e.Kind {
		case KindAdded:
			added++
			sym, symStyle = "+", ui.Green
			oldCell, newCell = dash, ui.Text(format(e.New), ui.Green)
		case KindRemoved:
			removed++
			sym, symStyle = "-", ui.Red
			oldCell, newCell = ui.Text(format(e.Old), ui.Red), dash
		default:
			changed++
			oldCell, newCell = wordDiff(format(e.Old), format(e.New))
		}
		table.Rows = append(table.Rows, []ui.Cell{
			ui.Text(sym, symStyle),
			ui.Text(e.PathStr(), ui.Bold),
			oldCell,
			newCell,
		})
	}

	renderIgnored(o.Ignored)
	table.Render(ui.Err)
	ui.Print("")
	renderSummary(added, removed, changed)
}

// wordDiff highlights where two values differ.
//
// ponytail: common prefix/suffix rather than a full LCS. For config values
// (t2.micro → t3.small, dev-api → prod-api) it marks the same span a character
// diff would; swap in an LCS if interior common runs ever need highlighting.
func wordDiff(oldStr, newStr string) (ui.Cell, ui.Cell) {
	a, b := []rune(oldStr), []rune(newStr)

	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix &&
		a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}

	build := func(runes []rune, base, highlight ui.Style) ui.Cell {
		var cell ui.Cell
		if prefix > 0 {
			cell = append(cell, ui.Segment{Text: string(runes[:prefix]), Style: base})
		}
		if middle := string(runes[prefix : len(runes)-suffix]); middle != "" {
			cell = append(cell, ui.Segment{Text: middle, Style: highlight})
		}
		if suffix > 0 {
			cell = append(cell, ui.Segment{Text: string(runes[len(runes)-suffix:]), Style: base})
		}
		if len(cell) == 0 {
			return ui.Text("", base)
		}
		return cell
	}

	return build(a, ui.Red, ui.DelHighlight), build(b, ui.Green, ui.AddHighlight)
}

// ── json ──────────────────────────────────────────────────────────────────────

type jsonFileMeta struct {
	File   string `json:"file"`
	Branch string `json:"branch,omitempty"`
}

type jsonEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Old  any    `json:"old"`
	New  any    `json:"new"`
}

type jsonOutput struct {
	Files struct {
		Old jsonFileMeta `json:"old"`
		New jsonFileMeta `json:"new"`
	} `json:"files"`
	Diffs []jsonEntry `json:"diffs"`
}

func renderJSON(entries []Entry, o RenderOptions) {
	out := jsonOutput{Diffs: []jsonEntry{}}
	out.Files.Old = jsonFileMeta{File: o.FileA, Branch: o.BranchA}
	out.Files.New = jsonFileMeta{File: o.FileB, Branch: o.BranchB}
	for _, e := range sortByPath(entries) {
		out.Diffs = append(out.Diffs, jsonEntry{
			Path: e.PathStr(), Kind: string(e.Kind), Old: e.Old, New: e.New,
		})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		ui.Error("could not encode diff as JSON: %v", err)
		return
	}
	fmt.Fprint(ui.Out, buf.String())
}

// ── shared ────────────────────────────────────────────────────────────────────

func renderIgnored(ignored []Entry) {
	if len(ignored) == 0 {
		return
	}
	ui.Print(ui.Dim.Sprintf("⊘  Ignored (%d) — matched ignore rules", len(ignored)))
	for _, e := range sortByPath(ignored) {
		ui.Print(ui.Dim.Render("   ~ " + e.PathStr()))
	}
	ui.Print("")
}

func renderSummary(added, removed, changed int) {
	ui.Columns(ui.Err,
		[]string{"+  added", "-  removed", "~  changed"},
		[]string{strconv.Itoa(added), strconv.Itoa(removed), strconv.Itoa(changed)},
		[]ui.Style{ui.BoldGreen, ui.BoldRed, ui.BoldYellow},
	)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case int64:
		return "int"
	case float64:
		return "float"
	case string:
		return "string"
	case map[string]any:
		return "map"
	case []any:
		return "list"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// formatValue renders a value for display: strings quoted, containers as
// compact JSON, everything else as-is.
func formatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return "'" + t + "'"
	case map[string]any, []any:
		return compactJSON(t)
	default:
		return fmt.Sprint(t)
	}
}

// formatHCL renders a value as an HCL literal.
func formatHCL(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return `"` + t + `"`
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case []any:
		items := make([]string, len(t))
		for i, item := range t {
			items[i] = formatHCL(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]any:
		pairs := make([]string, 0, len(t))
		for _, k := range slices.Sorted(maps.Keys(t)) {
			pairs = append(pairs, fmt.Sprintf("%s = %s", k, formatHCL(t[k])))
		}
		if len(pairs) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(pairs, " ") + " }"
	default:
		return fmt.Sprint(t)
	}
}

// compactJSON renders a container on one line. The error is dropped because
// everything reaching here has been through normalize: maps, slices and scalars
// only, nothing json.Marshal can refuse.
func compactJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(buf.String(), "\n")
}
