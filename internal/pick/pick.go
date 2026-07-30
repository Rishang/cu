// Package pick runs fzf in-process for interactive selection.
//
// fzf is imported as a library rather than shelled out to: it reads items from
// a channel and writes selections to another, so there is no subprocess, no
// "fzf not found" path, and the user's own $FZF_DEFAULT_OPTS still applies.
package pick

import (
	"fmt"

	fzf "github.com/junegunn/fzf/src"
)

// Options tunes a single fzf invocation.
type Options struct {
	Multi  bool
	Prompt string
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
		if _, seen := index[labels[i]]; !seen {
			index[labels[i]] = i
		}
	}

	args := []string{"--exact"}
	if opts.Multi {
		args = append(args, "--multi")
	}
	if opts.Prompt != "" {
		args = append(args, "--prompt="+opts.Prompt)
	}
	lines, err := runFZF(labels, args)
	if err != nil {
		return nil, err
	}

	var selected []T
	for _, line := range lines {
		if i, ok := index[line]; ok {
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
