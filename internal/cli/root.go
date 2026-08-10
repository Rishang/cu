// Package cli wires up the cu command tree.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Rishang/cloudutil/internal/pick"
	"github.com/Rishang/cloudutil/internal/ui"
)

// ExitCodeError carries a specific process exit code out of a command, for
// cases like cu diff exiting 1 when differences were found.
type ExitCodeError struct {
	Code int
}

func (e ExitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// exitWith returns an error that terminates cu with the given code and no
// additional message.
func exitWith(code int) error { return ExitCodeError{Code: code} }

// pickFrom is the list → fzf → selection flow every interactive command shares.
// An empty result means there was nothing to pick or the user picked nothing;
// both are reported here and are not errors. noun is singular, e.g. "AWS secret".
func pickFrom[T any](items []T, label func(T) string, noun string, opts pick.Options) ([]T, error) {
	if len(items) == 0 {
		ui.Warn("No %ss found.", noun)
		return nil, nil
	}

	ui.Info("Found %d %s(s). Opening fzf for selection...", len(items), noun)
	selected, err := pick.Select(items, label, opts)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		ui.Warn("No selection made.")
	}
	return selected, nil
}

// pickStrings is pickFrom for plain string lists.
func pickStrings(items []string, noun string, opts pick.Options) ([]string, error) {
	return pickFrom(items, itself, noun, opts)
}

// itself labels a string list with itself.
func itself(s string) string { return s }

// pickAndPrint is the browse-a-secret-store flow: multi-select from items, fetch
// each selection, and print the results as one JSON object. fetch returns the
// key to file the result under along with the value, since what names an entry
// differs per store.
func pickAndPrint[T, V any](items []T, label func(T) string, noun, prompt string,
	fetch func(T) (string, V, error),
) error {
	selected, err := pickFrom(items, label, noun, pick.Options{Multi: true, Prompt: prompt})
	if err != nil || len(selected) == 0 {
		return err
	}

	payload := make(map[string]V, len(selected))
	for _, item := range selected {
		key, value, err := fetch(item)
		if err != nil {
			return err
		}
		payload[key] = value
	}
	return ui.PrintJSON(payload)
}

// rootName is the installed binary name, used wherever help text has to spell
// out a command the user should type.
const rootName = "cu"

// NewRootCommand builds the cu command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   rootName,
		Short: "Cloud and Kubernetes utilities for everyday operations",
		Long: "cu bundles the AWS, Azure, Kubernetes and config-diffing chores that " +
			"otherwise turn into one-off shell scripts.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newAWSCommand(),
		newAzureCommand(),
		newOSCommand(),
		newK8sCommand(),
		newPwpushCommand(),
		newVaultCommand(),
		newDiffCommand(),
		newTaskCommand(),
		newJSON2YAMLCommand(),
		newYAML2JSONCommand(),
	)
	return root
}
