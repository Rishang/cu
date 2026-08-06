// Package pick runs fzf in-process for interactive selection.
//
// fzf is imported as a library rather than shelled out to: it reads items from
// a channel and writes selections to another, so there is no subprocess, no
// "fzf not found" path, and the user's own $FZF_DEFAULT_OPTS still applies.
package pick

import (
	"fmt"

	fzf "github.com/junegunn/fzf/src"

	"github.com/Rishang/cloudutil/internal/ui"
)

// Options tunes a single fzf invocation.
type Options struct {
	Multi  bool
	Prompt string
	// Preview is a shell command whose output fills a side panel for whichever
	// line is highlighted. fzf substitutes {} with that line, already quoted.
	Preview string
}

// fzfArgs turns Options into fzf's own flags.
func fzfArgs(opts Options) []string {
	// --ansi so a label can highlight itself, e.g. the active kube context.
	// fzf strips the codes from what it returns, which is why Select indexes
	// items by their stripped label.
	args := []string{"--exact", "--ansi"}
	if opts.Multi {
		args = append(args, "--multi")
	}
	if opts.Prompt != "" {
		args = append(args, "--prompt="+opts.Prompt)
	}
	if opts.Preview != "" {
		args = append(args,
			"--preview="+opts.Preview,
			// One fixed position, no responsive alternative: fzf re-resolves a
			// size threshold every frame, so a pane sitting near the boundary
			// flips layout as you type. The border doubles as the drag handle
			// for resizing, and mouse support is on by default.
			"--preview-window=right,50%,wrap",
			// A leading arrow on every continuation line is noisy for wrapped
			// logs; indent them instead.
			"--preview-wrap-sign=  ",
			// The preview costs a subprocess per cursor move, so make it easy
			// to get rid of, and give the mouseless a way to resize it.
			"--bind=ctrl-o:toggle-preview",
			"--bind=ctrl-]:change-preview-window(right,75%|right,25%|right,50%)")
	}
	return args
}

// Select presents items in fzf and returns the ones chosen. An empty result
// means the user picked nothing (escape, ctrl-c, or no match), which is not an
// error.
func Select[T any](items []T, label func(T) string, opts Options) ([]T, error) {
	if len(items) == 0 {
		return nil, nil
	}

	labels := make([]string, len(items))
	index := make(map[string]int, len(items))
	for i, item := range items {
		labels[i] = label(item)
		// Keyed on the visible text: fzf hands back the line with any styling
		// already removed.
		plain := ui.StripANSI(labels[i])
		if _, seen := index[plain]; !seen {
			index[plain] = i
		}
	}

	lines, err := runFZF(labels, fzfArgs(opts))
	if err != nil {
		return nil, err
	}

	var selected []T
	for _, line := range lines {
		if i, ok := index[ui.StripANSI(line)]; ok {
			selected = append(selected, items[i])
		}
	}
	return selected, nil
}

// runFZF feeds labels to fzf over a channel and returns the selected lines. No
// subprocess is involved; fzf opens the tty itself for its UI, leaving our
// stdin and stdout alone.
func runFZF(labels []string, args []string) ([]string, error) {
	opts, err := fzf.ParseOptions(true, args)
	if err != nil {
		return nil, fmt.Errorf("fzf options: %w", err)
	}

	in := make(chan string, len(labels))
	for _, label := range labels {
		in <- label
	}
	close(in)

	// Buffered to len(labels): fzf can never emit more lines than it was given,
	// so it cannot block on Output while we wait for Run to return.
	out := make(chan string, len(labels))
	opts.Input = in
	opts.Output = out

	code, err := fzf.Run(opts)
	close(out)
	// fzf redraws the tty itself and can hand back control mid-line, so the
	// next thing we print lands wherever its cursor was left.
	ui.ResetCursor()
	if err != nil {
		return nil, fmt.Errorf("fzf: %w", err)
	}
	switch code {
	case fzf.ExitOk, fzf.ExitNoMatch, fzf.ExitInterrupt:
	default:
		return nil, fmt.Errorf("fzf exited with code %d", code)
	}

	var lines []string
	for line := range out {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
