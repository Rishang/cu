// Package ui centralizes terminal output. Status messages go to stderr so that
// machine-readable payloads on stdout stay clean and pipeable.
//
// Styling is plain ANSI rather than a styling library: fzf (which must be
// embedded) and lipgloss v1 require incompatible versions of charmbracelet/x/ansi,
// and the output here needs only colors, a rule and one table.
package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// Out carries data (JSON payloads, generated URLs); Err carries status text.
var (
	Out io.Writer = os.Stdout
	Err io.Writer = os.Stderr
)

// ResetCursor returns the cursor to column 0. A child process handed the tty
// back can leave it mid-line, which would otherwise indent whatever prints next.
func ResetCursor() { fmt.Fprint(Err, "\r") }

// Style is a set of ANSI SGR parameters, e.g. "1;31" for bold red.
type Style struct{ code string }

// Render wraps text in this style's escape codes, or returns it unchanged when
// color is disabled.
func (s Style) Render(text string) string {
	if !colorEnabled || s.code == "" || text == "" {
		return text
	}
	return "\x1b[" + s.code + "m" + text + "\x1b[0m"
}

// Sprintf renders formatted text in this style.
func (s Style) Sprintf(format string, a ...any) string {
	return s.Render(fmt.Sprintf(format, a...))
}

// Styles use ANSI 30-37 so they follow the user's terminal theme. The word-diff
// highlights need specific dark backgrounds, so those use truecolor.
var (
	Plain      = Style{}
	Red        = Style{"31"}
	Green      = Style{"32"}
	Yellow     = Style{"33"}
	Cyan       = Style{"36"}
	Blue       = Style{"34"}
	Bold       = Style{"1"}
	Dim        = Style{"2"}
	DimItalic  = Style{"2;3"}
	BoldRed    = Style{"1;31"}
	BoldGreen  = Style{"1;32"}
	BoldYellow = Style{"1;33"}
	BoldCyan   = Style{"1;36"}
	// Differing spans inside a changed value.
	DelHighlight = Style{"31;48;2;58;20;20"}
	AddHighlight = Style{"32;48;2;20;58;28"}
)

var colorEnabled = defaultColor()

func defaultColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// SetColor forces color output on or off, backing --color/--no-color.
func SetColor(enabled bool) { colorEnabled = enabled }

// ColorEnabled reports whether styling is currently applied.
func ColorEnabled() bool { return colorEnabled }

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// TextWidth returns the rendered column width of s, for callers doing their own
// alignment. Not to be confused with width(), the terminal's width.
func TextWidth(s string) int { return visibleWidth(s) }

// StripANSI removes styling from s, leaving the text a terminal would show.
func StripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// visibleWidth returns the rendered column width of s, ignoring escape codes.
func visibleWidth(s string) int {
	return runewidth.StringWidth(ansiPattern.ReplaceAllString(s, ""))
}

// width returns the terminal width, defaulting to 80 when it is unknown.
// $COLUMNS wins, so output stays reproducible when piped or under test.
func width() int {
	if w, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && w > 0 {
		return w
	}
	for _, f := range []*os.File{os.Stderr, os.Stdout} {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

// Print writes a line to stderr.
func Print(a ...any) { fmt.Fprintln(Err, a...) }

// Printf writes formatted text to stderr, appending a newline.
func Printf(format string, a ...any) { fmt.Fprintf(Err, format+"\n", a...) }

// Info reports progress, e.g. "[*] Listing secrets".
func Info(format string, a ...any) { Printf("[*] "+format, a...) }

// Ok reports success.
func Ok(format string, a ...any) { Printf(Green.Render("[+]")+" "+format, a...) }

// Warn reports a recoverable problem.
func Warn(format string, a ...any) { Print(Yellow.Sprintf("[!] "+format, a...)) }

// Error reports a failure.
func Error(format string, a ...any) {
	Printf("%s %s", BoldRed.Render("[ERROR]"), fmt.Sprintf(format, a...))
}

// outIsTerminal reports whether Out is a real terminal. Out is redirected in
// tests and whenever stdout is piped, and neither should get escape codes.
func outIsTerminal() bool {
	f, ok := Out.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// PrintJSON writes v to stdout as JSON: syntax-highlighted and indented for a
// human at a terminal, compact when piped into another tool.
func PrintJSON(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return PrintJSONBytes(buf.Bytes())
}

// PrintJSONBytes is PrintJSON for JSON that is already encoded. Callers that
// care about key order need this one: routing through PrintJSON would decode
// into a map first, and Go sorts map keys on the way back out.
func PrintJSONBytes(data []byte) error {
	data = bytes.TrimRight(data, "\n")

	if !outIsTerminal() {
		_, err := fmt.Fprintf(Out, "%s\n", data)
		return err
	}
	if !colorEnabled {
		var indented bytes.Buffer
		if err := json.Indent(&indented, data, "", "  "); err != nil {
			return err
		}
		_, err := fmt.Fprintf(Out, "%s\n", indented.Bytes())
		return err
	}
	return colorJSON(Out, data)
}

// JSON token colors, roughly matching what rich prints.
var (
	jsonKey   = BoldCyan
	jsonStr   = Green
	jsonNum   = Yellow
	jsonConst = DimItalic // true, false, null
	jsonPunct = Dim
)

// colorJSON re-emits already-valid JSON with two-space indentation and per-token
// color. It streams tokens instead of walking a decoded value so that key order
// and number formatting survive untouched.
func colorJSON(w io.Writer, data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var stack []bool // one entry per open container; true when it is an object
	first, afterKey := true, false

	// pre writes whatever separates this token from the previous one.
	pre := func() {
		if afterKey {
			fmt.Fprint(w, jsonPunct.Render(": "))
			afterKey = false
			return
		}
		if !first {
			fmt.Fprint(w, jsonPunct.Render(","))
		}
		if len(stack) > 0 {
			fmt.Fprint(w, "\n"+strings.Repeat("  ", len(stack)))
		}
		first = false
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			_, err := fmt.Fprintln(w)
			return err
		}
		if err != nil {
			return err
		}

		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				pre()
				fmt.Fprint(w, jsonPunct.Render(string(delim)))
				stack = append(stack, delim == '{')
				first = true
			default:
				empty := first
				stack = stack[:len(stack)-1]
				if !empty {
					fmt.Fprint(w, "\n"+strings.Repeat("  ", len(stack)))
				}
				fmt.Fprint(w, jsonPunct.Render(string(delim)))
				first = false
			}
			continue
		}

		// Inside an object, tokens alternate key/value, so anything that does
		// not follow a key is a key.
		isKey := len(stack) > 0 && stack[len(stack)-1] && !afterKey
		pre()

		switch t := tok.(type) {
		case string:
			if isKey {
				fmt.Fprint(w, jsonKey.Render(jsonQuote(t)))
				afterKey = true
			} else {
				fmt.Fprint(w, jsonStr.Render(jsonQuote(t)))
			}
		case json.Number:
			fmt.Fprint(w, jsonNum.Render(t.String()))
		case bool:
			fmt.Fprint(w, jsonConst.Render(strconv.FormatBool(t)))
		default: // nil
			fmt.Fprint(w, jsonConst.Render("null"))
		}
	}
}

// jsonQuote renders s as a JSON string literal without HTML escaping.
func jsonQuote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Rule draws a full-width horizontal rule with a centered title. The title is
// expected to be pre-styled; line styles the dashes.
func Rule(title string, line Style) {
	w := width()
	if title == "" {
		Print(line.Render(strings.Repeat("─", w)))
		return
	}

	label := " " + title + " "
	inner := visibleWidth(label)
	if inner >= w {
		Print(label)
		return
	}
	left := (w - inner) / 2
	Print(line.Render(strings.Repeat("─", left)) + label +
		line.Render(strings.Repeat("─", w-inner-left)))
}
