// Package kube reads Kubernetes objects through the kubectl binary.
//
// kubectl stays an external dependency on purpose: it is already installed
// wherever these commands are useful, and it carries the user's kubeconfig,
// contexts, auth plugins and proxy settings without cu having to model any of it.
package kube

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// KeyRef identifies a single data key inside a Secret or ConfigMap.
type KeyRef struct {
	Namespace string
	Name      string
	Key       string
}

// Display is the fzf line for this key: namespace/name/key.
func (k KeyRef) Display() string {
	return fmt.Sprintf("%s/%s/%s", k.Namespace, k.Name, k.Key)
}

// kubectl builds a kubectl command carrying the environment it should run with,
// which is the ambient one everywhere except inside a pod with no kubeconfig.
func kubectl(args ...string) *exec.Cmd {
	cmd := exec.Command("kubectl", args...)
	cmd.Env = kubectlEnv()
	return cmd
}

// run executes kubectl and returns its stdout, turning a failure into an error
// carrying kubectl's own message.
func run(args ...string) ([]byte, error) {
	out, err := kubectl(args...).Output()
	if err == nil {
		return out, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		detail := cmp.Or(strings.TrimSpace(string(exitErr.Stderr)), strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("kubectl %s failed: %s", strings.Join(args, " "), detail)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("kubectl not found in PATH")
	}
	return nil, fmt.Errorf("kubectl %s failed: %w", strings.Join(args, " "), err)
}

// runJSON executes kubectl with -o json and decodes the result into T.
func runJSON[T any](args ...string) (T, error) {
	var data T
	out, err := run(args...)
	if err != nil {
		return data, err
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return data, fmt.Errorf("failed to parse kubectl output as JSON: %w", err)
	}
	return data, nil
}

// objectMeta is the part of a Kubernetes object's metadata cu reads. Callers
// mirror kubectl and read an empty Namespace as "default": the object came
// from a request that had already scoped one.
type objectMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// runLines executes kubectl and returns its non-blank stdout lines, for
// queries that are just a list of names. Split on newlines only, not
// strings.Fields: a context name may contain a space.
func runLines(args ...string) ([]string, error) {
	out, err := run(args...)
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' }), nil
}

// ListContexts returns context names from the current kubeconfig.
func ListContexts() ([]string, error) {
	return runLines("config", "get-contexts", "-o", "name")
}

// configValue runs a read-only kubectl config query. A failure — no kubeconfig,
// nothing selected — reads as "unset" rather than an error: callers only use
// these to mark the active entry in a list.
func configValue(args ...string) string {
	out, err := run(args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CurrentContext returns the context kubectl would act on.
func CurrentContext() string { return configValue("config", "current-context") }

// CurrentNamespace returns the current context's default namespace, which is
// what a bare kubectl get would search. An unset one falls back to default,
// just as kubectl does.
func CurrentNamespace() string {
	return cmp.Or(configValue("config", "view", "--minify", "-o", "jsonpath={..namespace}"), "default")
}

// ListNamespaces returns every namespace name. jsonpath rather than `-o name`,
// which prefixes each line with "namespace/". The API returns them sorted and
// names are unique, so there is nothing to sort or dedupe here.
func ListNamespaces() ([]string, error) {
	return runLines("get", "namespaces", "-o",
		`jsonpath={range .items[*]}{.metadata.name}{"\n"}{end}`)
}

// scopeArgs appends the namespace selector kubectl needs.
func scopeArgs(args []string, allNamespaces bool, namespace string) []string {
	switch {
	case allNamespaces:
		return append(args, "-A")
	case namespace != "":
		return append(args, "-n", namespace)
	}
	return args
}

// keyedItem covers Secrets and ConfigMaps alike: both carry data, and a
// ConfigMap also binaryData. A Secret's stringData is not here because it is
// write-only — the API server folds it into data and never returns it.
type keyedItem struct {
	Metadata   objectMeta        `json:"metadata"`
	Data       map[string]string `json:"data"`
	BinaryData map[string]string `json:"binaryData"`
}

// ListKeys returns one entry per data key of every Secret or ConfigMap in
// scope; resource is what kubectl calls them.
func ListKeys(resource string, allNamespaces bool, namespace string) ([]KeyRef, error) {
	args := scopeArgs([]string{"get", resource, "-o", "json"}, allNamespaces, namespace)

	list, err := runJSON[struct{ Items []keyedItem }](args...)
	if err != nil {
		return nil, err
	}

	var refs []KeyRef
	for _, item := range list.Items {
		if item.Metadata.Name == "" {
			continue
		}
		unique := map[string]struct{}{}
		for _, data := range []map[string]string{item.Data, item.BinaryData} {
			for key := range data {
				unique[key] = struct{}{}
			}
		}
		for _, key := range slices.Sorted(maps.Keys(unique)) {
			refs = append(refs, KeyRef{
				Namespace: cmp.Or(item.Metadata.Namespace, "default"),
				Name:      item.Metadata.Name,
				Key:       key,
			})
		}
	}
	return refs, nil
}

// SecretValue fetches one Secret key, base64-decoding data entries.
func SecretValue(ref KeyRef) (string, error) {
	secret, err := runJSON[keyedItem]("get", "secret", ref.Name, "-n", ref.Namespace, "-o", "json")
	if err != nil {
		return "", err
	}
	if encoded, ok := secret.Data[ref.Key]; ok {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return fmt.Sprintf("<b64-decode-error> raw=%q", encoded), nil
		}
		return string(decoded), nil
	}
	return "<key not found>", nil
}

// ConfigMapValue fetches one ConfigMap key. Binary entries are summarized
// rather than dumped.
func ConfigMapValue(ref KeyRef) (string, error) {
	cm, err := runJSON[keyedItem]("get", "configmap", ref.Name, "-n", ref.Namespace, "-o", "json")
	if err != nil {
		return "", err
	}
	if value, ok := cm.Data[ref.Key]; ok {
		return value, nil
	}
	if encoded, ok := cm.BinaryData[ref.Key]; ok {
		return fmt.Sprintf("<binaryData base64, length %d chars>", len(encoded)), nil
	}
	return "<key not found>", nil
}

// Pod is a pod, its containers and its current phase.
type Pod struct {
	Namespace  string
	Name       string
	Phase      string
	Containers []string
}

// Display is the fzf line for this pod: namespace/name (phase). The phase is
// there so a CrashLoopBackOff pod is pickable by typing "crash".
func (p Pod) Display() string {
	return fmt.Sprintf("%s/%s (%s)", p.Namespace, p.Name, p.Phase)
}

// podList is the shape of `kubectl get pods -o json`, trimmed to what cu shows.
type podList struct {
	Items []struct {
		Metadata objectMeta `json:"metadata"`
		Spec     struct {
			// Init containers come first so the order matches what
			// --all-containers emits.
			InitContainers []struct{ Name string } `json:"initContainers"`
			Containers     []struct{ Name string } `json:"containers"`
		} `json:"spec"`
		Status struct{ Phase string } `json:"status"`
	} `json:"items"`
}

// ListPods returns pods with their containers and phase.
func ListPods(allNamespaces bool, namespace string) ([]Pod, error) {
	list, err := runJSON[podList](scopeArgs([]string{"get", "pods", "-o", "json"}, allNamespaces, namespace)...)
	if err != nil {
		return nil, err
	}
	return parsePods(list), nil
}

func parsePods(list podList) []Pod {
	var pods []Pod
	for _, item := range list.Items {
		// A pod with no name cannot be passed to kubectl logs, so drop it.
		if item.Metadata.Name == "" {
			continue
		}
		pod := Pod{
			Namespace: cmp.Or(item.Metadata.Namespace, "default"),
			Name:      item.Metadata.Name,
			Phase:     cmp.Or(item.Status.Phase, "Unknown"),
		}
		for _, c := range slices.Concat(item.Spec.InitContainers, item.Spec.Containers) {
			if c.Name != "" {
				pod.Containers = append(pod.Containers, c.Name)
			}
		}
		pods = append(pods, pod)
	}
	return pods
}

// StreamLogs runs `kubectl logs` for one pod, writing straight through to w.
//
// kubectl's own --prefix labels the lines when the pod has several containers;
// there is no reason to reimplement it here. tail is passed through untouched,
// where -1 means the whole log.
func StreamLogs(pod Pod, follow bool, tail int, w io.Writer) error {
	args := []string{
		"logs", pod.Name, "-n", pod.Namespace,
		"--all-containers=true", "--tail=" + strconv.Itoa(tail),
	}
	if follow {
		args = append(args, "--follow")
	}
	if len(pod.Containers) > 1 {
		args = append(args, "--prefix")
	}

	cmd := kubectl(args...)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr // kubectl explains its own failures
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl logs %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}

// Exec runs a command in a pod with the process's standard streams attached.
func Exec(pod Pod, container string, command []string) error {
	// -t only when stdin really is a terminal; otherwise kubectl warns and
	// drops it, and piped input would arrive mangled by tty processing.
	args := execArgs(pod, container, command, term.IsTerminal(int(os.Stdin.Fd())))

	cmd := kubectl(args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// execArgs builds the kubectl exec argv.
func execArgs(pod Pod, container string, command []string, tty bool) []string {
	args := []string{"exec", "-i"}
	if tty {
		args = append(args, "-t")
	}
	if container != "" {
		args = append(args, "-c", container)
	}
	args = append(args, "-n", pod.Namespace, pod.Name, "--")
	return append(args, command...)
}

// UseContext switches the current kubeconfig context.
func UseContext(name string) error {
	return configure("config", "use-context", name)
}

// UseNamespace sets the default namespace on the current context.
func UseNamespace(name string) error {
	return configure("config", "set-context", "--current", "--namespace", name)
}

// configure runs a kubectl config subcommand, relaying kubectl's confirmation
// to stderr so stdout stays free for data.
func configure(args ...string) error {
	out, err := run(args...)
	if len(out) > 0 {
		fmt.Fprint(os.Stderr, string(out))
	}
	return err
}
