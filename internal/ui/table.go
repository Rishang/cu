package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Segment is a run of text sharing one style. Cells are built from segments so
// that a single cell can carry per-span highlighting (the word-diff colors)
// and still be folded across lines correctly.
type Segment struct {
	Text  string
	Style Style
}

// Cell is one table cell.
type Cell []Segment

// Text builds a single-style cell.
func Text(s string, style Style) Cell { return Cell{{Text: s, Style: style}} }

// width returns the cell's unfolded display width.
func (c Cell) width() int {
	w := 0
	for _, seg := range c {
		w += runewidth.StringWidth(seg.Text)
	}
	return w
}

// fold hard-wraps the cell at width columns, preserving per-segment styling.
// Hard folding (rather than word wrapping) matches the values being displayed:
// paths and JSON blobs have no useful break points.
func (c Cell) fold(width int) []string {
	if width < 1 {
		width = 1
	}
	var (
		lines []string
		cur   strings.Builder
		curw  int
	)
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		curw = 0
	}
	for _, seg := range c {
		var chunk strings.Builder
		for _, r := range seg.Text {
			rw := runewidth.RuneWidth(r)
			if curw+rw > width {
				cur.WriteString(seg.Style.Render(chunk.String()))
				chunk.Reset()
				flush()
			}
			chunk.WriteRune(r)
			curw += rw
		}
		cur.WriteString(seg.Style.Render(chunk.String()))
	}
	flush()
	return lines
}

// Align controls horizontal placement within a column.
type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// Table renders rows inside a rounded box, folding cells to fit the terminal.
//
// ponytail: no zebra striping — the Python version alternated row backgrounds,
// which does not survive folding cleanly. Borders plus per-kind colors carry
// the same information.
type Table struct {
	Headers []Cell
	Rows    [][]Cell
	Aligns  []Align
	// NoShrink marks columns that should keep their natural width, so folding
	// falls on the value columns rather than on paths. They are only shrunk as
	// a last resort, when the other columns are already at their floor.
	NoShrink []bool
	MaxWidth int   // total width, defaults to the terminal width
	Border   Style // style for the box drawing characters
}

const (
	tblTopLeft  = "╭"
	tblTopRight = "╮"
	tblBotLeft  = "╰"
	tblBotRight = "╯"
	tblHoriz    = "─"
	tblVert     = "│"
	tblTopTee   = "┬"
	tblBotTee   = "┴"
	tblLeftTee  = "├"
	tblRightTee = "┤"
	tblCross    = "┼"
)

// Render writes the table to w.
func (t *Table) Render(w io.Writer) {
	cols := len(t.Headers)
	if cols == 0 {
		return
	}
	widths := t.columnWidths(cols)

	border := func(left, mid, right string) {
		parts := make([]string, cols)
		for i, cw := range widths {
			parts[i] = strings.Repeat(tblHoriz, cw+2)
		}
		fmt.Fprintln(w, t.Border.Render(left+strings.Join(parts, mid)+right))
	}

	border(tblTopLeft, tblTopTee, tblTopRight)
	t.renderRow(w, t.Headers, widths)
	border(tblLeftTee, tblCross, tblRightTee)
	for _, row := range t.Rows {
		t.renderRow(w, row, widths)
	}
	border(tblBotLeft, tblBotTee, tblBotRight)
}

func (t *Table) columnWidths(cols int) []int {
	widths := make([]int, cols)
	for i, h := range t.Headers {
		widths[i] = h.width()
	}
	for _, row := range t.Rows {
		for i, c := range row {
			if i < cols && c.width() > widths[i] {
				widths[i] = c.width()
			}
		}
	}

	maxWidth := t.MaxWidth
	if maxWidth <= 0 {
		maxWidth = width()
	}

	// Overhead: one vertical rule per column plus a closing one, and a space of
	// padding on each side of every cell.
	budget := maxWidth - (cols + 1) - 2*cols
	for total(widths) > budget {
		idx := t.widestShrinkable(widths, false)
		if idx < 0 {
			// Only protected columns are left with room to give.
			idx = t.widestShrinkable(widths, true)
		}
		if idx < 0 {
			break // every column is at its floor
		}
		widths[idx]--
	}
	return widths
}

// minColumnWidth is the floor a column is never shrunk below.
const minColumnWidth = 6

// widestShrinkable returns the index of the widest column still above the floor,
// skipping protected columns unless includeProtected is set.
func (t *Table) widestShrinkable(widths []int, includeProtected bool) int {
	widest, idx := 0, -1
	for i, cw := range widths {
		if cw <= minColumnWidth || cw <= widest {
			continue
		}
		if !includeProtected && i < len(t.NoShrink) && t.NoShrink[i] {
			continue
		}
		widest, idx = cw, i
	}
	return idx
}

func total(widths []int) int {
	sum := 0
	for _, w := range widths {
		sum += w
	}
	return sum
}

func (t *Table) renderRow(w io.Writer, row []Cell, widths []int) {
	folded := make([][]string, len(widths))
	height := 1
	for i := range widths {
		var cell Cell
		if i < len(row) {
			cell = row[i]
		}
		folded[i] = cell.fold(widths[i])
		if len(folded[i]) > height {
			height = len(folded[i])
		}
	}

	vert := t.Border.Render(tblVert)
	for line := range height {
		var b strings.Builder
		b.WriteString(vert)
		for i, cw := range widths {
			content := ""
			if line < len(folded[i]) {
				content = folded[i][line]
			}
			b.WriteString(" ")
			b.WriteString(t.pad(content, cw, i))
			b.WriteString(" ")
			b.WriteString(vert)
		}
		fmt.Fprintln(w, b.String())
	}
}

func (t *Table) pad(content string, width, col int) string {
	gap := width - visibleWidth(content)
	if gap < 0 {
		gap = 0
	}
	align := AlignLeft
	if col < len(t.Aligns) {
		align = t.Aligns[col]
	}
	switch align {
	case AlignCenter:
		left := gap / 2
		return strings.Repeat(" ", left) + content + strings.Repeat(" ", gap-left)
	case AlignRight:
		return strings.Repeat(" ", gap) + content
	default:
		return content + strings.Repeat(" ", gap)
	}
}

// Columns renders label/value pairs as a borderless summary block, matching the
// counts line the Python renderer printed under each diff.
func Columns(w io.Writer, headers []string, values []string, styles []Style) {
	if len(headers) == 0 {
		return
	}
	widths := make([]int, len(headers))
	for i := range headers {
		widths[i] = max(visibleWidth(headers[i]), visibleWidth(values[i]))
	}

	var head, body strings.Builder
	for i := range headers {
		head.WriteString(Dim.Render(padRight(headers[i], widths[i])))
		head.WriteString("   ")
		style := Plain
		if i < len(styles) {
			style = styles[i]
		}
		body.WriteString(style.Render(padRight(values[i], widths[i])))
		body.WriteString("   ")
	}
	fmt.Fprintln(w, strings.TrimRight(head.String(), " "))
	fmt.Fprintln(w, strings.TrimRight(body.String(), " "))
}

func padRight(s string, width int) string {
	if gap := width - visibleWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
