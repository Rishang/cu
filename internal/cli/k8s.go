package cli

import (
	"io"
	"os"

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
			"context", kube.ListContexts, kube.UseContext),
		newKubeSwitchCommand("ns", "Switch the current context's namespace interactively",
			"namespace", kube.ListNamespaces, kube.UseNamespace),
	)
	return cmd
}

func newKubeLogsCommand() *cobra.Command {
	var (
		allNamespaces bool
		namespace     string
		follow        bool
		outFile       string
		tail          int
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fuzzy-pick pods and stream their logs",
		Long: `Pick one or more pods with fzf and stream their logs.

Selecting several pods, or a pod with several containers, turns on kubectl's
--prefix so every line says where it came from. Lines from concurrent streams
are merged whole, never interleaved mid-line.

  cu k logs -n prod -f            # pick pods in prod, then follow
  cu k logs -A -f -o app.log      # follow across all namespaces, tee to a file`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if allNamespaces {
				namespace = ""
			}

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
				pick.Options{Multi: true, Prompt: "pod> "})
			if err != nil || len(selected) == 0 {
				return err
			}

			out := ui.Out
			if outFile != "" {
				f, err := os.Create(outFile)
				if err != nil {
					return err
				}
				defer f.Close()
				// Tee rather than redirect: watching the stream is the point,
				// keeping a copy is the extra.
				out = io.MultiWriter(ui.Out, f)
				ui.Info("Also writing to %s", ui.Cyan.Render(outFile))
			}
			return kube.StreamLogs(selected, follow, tail, out)
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search across all namespaces.")
	flags.StringVarP(&namespace, "namespace", "n", "",
		"Namespace to search (ignored with --all-namespaces).")
	flags.BoolVarP(&follow, "follow", "f", false, "Stream new log lines as they arrive.")
	flags.IntVarP(&tail, "tail", "t", -1, "Show only this many recent lines per pod (-1 for all).")
	flags.StringVarP(&outFile, "output", "o", "", "Also write the log stream to this file.")
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
			selected, err := pickFrom(refs, kube.KeyRef.Display, itemName, pick.Options{
				Multi:  true,
				Prompt: use + "> ",
			})
			if err != nil || len(selected) == 0 {
				return err
			}

			payload := map[string]string{}
			for _, ref := range selected {
				v, err := value(ref)
				if err != nil {
					return err
				}
				payload[ref.Display()] = v
			}
			return ui.PrintJSON(payload)
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
			selected, err := pickStrings(names, noun, pick.Options{Prompt: noun + "> "})
			if err != nil || len(selected) == 0 {
				return err
			}
			return switchTo(selected[0])
		},
	}
}
