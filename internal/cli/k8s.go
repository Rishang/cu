package cli

import (
	"github.com/spf13/cobra"

	"github.com/Rishang/cloudutil/internal/kube"
	"github.com/Rishang/cloudutil/internal/pick"
	"github.com/Rishang/cloudutil/internal/ui"
)

func newK8sCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "k",
		Short: "Kubernetes-related commands",
	}
	cmd.AddCommand(
		newK8sKeyCommand("secrets", "Interactive fzf view for Kubernetes secrets (base64-decoded)",
			"k8s secret key", kube.ListSecretKeys, kube.SecretValue),
		newK8sKeyCommand("configmaps", "Interactive fzf view for Kubernetes ConfigMaps",
			"k8s configmap key", kube.ListConfigMapKeys, kube.ConfigMapValue),
		newKubeLogsCommand(),
		newKubeSwitchCommand("ctx", "Switch between Kubernetes contexts interactively",
			"context", kube.ListContexts, kube.CurrentContext, kube.UseContext),
		newKubeSwitchCommand("ns", "Switch the current context's namespace interactively",
			"namespace", kube.ListNamespaces, kube.CurrentNamespace, kube.UseNamespace),
	)
	return cmd
}

// logsPreview fills the fzf side panel with the tail of whichever pod is
// highlighted. fzf substitutes {} with the selected line — "namespace/name
// (phase)", already shell-quoted — so the panel takes it back apart.
//
// The depth is fixed at 50 rather than following --tail: the panel is for
// telling pods apart, not for reading the log. 2>&1 so that a pod which is not
// ready yet explains itself in the panel instead of showing an empty box.
const logsPreview = `line={}; ns=${line%%/*}; rest=${line#*/}; ` +
	`kubectl logs --tail=50 --all-containers=true -n "$ns" "${rest%% *}" 2>&1`

func newKubeLogsCommand() *cobra.Command {
	var (
		allNamespaces bool
		namespace     string
		follow        bool
		tail          int
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fuzzy-pick a pod and stream its logs",
		Long: `Pick a pod with fzf and stream its logs.

The side panel shows the last 50 lines of whichever pod is highlighted, so you
can find the one misbehaving before committing to it. ctrl-o hides the panel.

A pod with several containers gets kubectl's --prefix, so every line says which
container it came from.

  cu k logs -n prod -f -t 100     # pick a pod in prod, follow the last 100 lines
  cu k logs -A -f > app.log       # search all namespaces, follow, redirect to a file`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// -A wins over -n inside kube.ListPods, so namespace needs no reset.
			pods, err := kube.ListPods(allNamespaces, namespace)
			if err != nil {
				return err
			}
			// Without -n the scope is whatever the context points at, which is
			// easy to forget; say so rather than just "no pods found".
			if len(pods) == 0 && !allNamespaces {
				scope := "the current namespace"
				if namespace != "" {
					scope = "namespace " + ui.Cyan.Render(namespace)
				}
				ui.Warn("No pods in %s. Use -A to search every namespace.", scope)
				return nil
			}

			selected, err := pickFrom(pods, kube.Pod.Display, "pod",
				pick.Options{Prompt: "pod> ", Preview: logsPreview})
			if err != nil || len(selected) == 0 {
				return err
			}

			return kube.StreamLogs(selected[0], follow, tail, ui.Out)
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search across all namespaces.")
	flags.StringVarP(&namespace, "namespace", "n", "",
		"Namespace to search (ignored with --all-namespaces).")
	flags.BoolVarP(&follow, "follow", "f", false, "Stream new log lines as they arrive.")
	flags.IntVarP(&tail, "tail", "t", -1, "Show only this many recent lines per pod (-1 for all).")
	return cmd
}

// newK8sKeyCommand builds the shared list → fzf → print flow used by both
// secrets and configmaps.
func newK8sKeyCommand(
	use, short, itemName string,
	list func(allNamespaces bool, namespace string) ([]kube.KeyRef, error),
	value func(kube.KeyRef) (string, error),
) *cobra.Command {
	var (
		allNamespaces   bool
		namespace       string
		selectNamespace bool
	)

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Unlike logs, the reset is load-bearing: it lets --select-namespace
			// still prompt when -A was passed alongside -n.
			if allNamespaces {
				namespace = ""
			}

			if selectNamespace && namespace == "" {
				namespaces, err := kube.ListNamespaces()
				if err != nil {
					return err
				}
				chosen, err := pickStrings(namespaces, "namespace", pick.Options{Prompt: "namespace> "})
				if err != nil || len(chosen) == 0 {
					return err
				}
				namespace, allNamespaces = chosen[0], false
			}

			refs, err := list(allNamespaces, namespace)
			if err != nil {
				return err
			}
			return pickAndPrint(refs, kube.KeyRef.Display, itemName, use+"> ",
				func(ref kube.KeyRef) (string, string, error) {
					v, err := value(ref)
					return ref.Display(), v, err
				})
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&allNamespaces, "all-namespaces", "A", false,
		"Search across all namespaces.")
	flags.StringVarP(&namespace, "namespace", "n", "",
		"Namespace to search (ignored with --all-namespaces).")
	flags.BoolVar(&selectNamespace, "select-namespace", false,
		"Use fzf to pick a namespace instead of scanning all of them.")
	return cmd
}

// newKubeSwitchCommand builds a list → fzf → switch command. Passing the name
// as an argument skips fzf, so `cu k ns prod` works from a script.
func newKubeSwitchCommand(
	use, short, noun string,
	list func() ([]string, error),
	active func() string,
	switchTo func(string) error,
) *cobra.Command {
	return &cobra.Command{
		Use:   use + " [name]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 {
				return switchTo(args[0])
			}
			names, err := list()
			if err != nil {
				return err
			}

			// Marking the active entry answers "which am I on?" without a
			// second command. Styling only touches the label, so what gets
			// switched to is still the plain name.
			current := active()
			selected, err := pickFrom(names, func(name string) string {
				return markCurrent(name, current)
			}, noun, pick.Options{Prompt: noun + "> "})
			if err != nil || len(selected) == 0 {
				return err
			}
			return switchTo(selected[0])
		},
	}
}

// markCurrent highlights name in green when it is the active one, falling back
// to a suffix when colour is off — under NO_COLOR that marker is the only thing
// left to tell them apart.
func markCurrent(name, current string) string {
	if name != current {
		return name
	}
	if !ui.ColorEnabled() {
		return name + " (current)"
	}
	return ui.Green.Render(name)
}
