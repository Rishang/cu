// Package kube reads Kubernetes objects through the kubectl binary.
//
// kubectl stays an external dependency on purpose: it is already installed
// wherever these commands are useful, and it carries the user's kubeconfig,
// contexts, auth plugins and proxy settings without cu having to model any of it.
package kube

import (
	"bufio"
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
	"sync"
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

// run executes kubectl and returns its stdout, turning a failure into an error
// carrying kubectl's own message.
func run(args ...string) ([]byte, error) {
	out, err := exec.Command("kubectl", args...).Output()
	if err == nil {
		return out, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		detail := strings.TrimSpace(string(exitErr.Stderr))
		if detail == "" {
			detail = strings.TrimSpace(string(out))
		}
		return nil, fmt.Errorf("kubectl %s failed: %s", strings.Join(args, " "), detail)
	}
	if _, lookErr := exec.LookPath("kubectl"); lookErr != nil {
		return nil, fmt.Errorf("kubectl not found in PATH")
	}
	return nil, fmt.Errorf("kubectl %s failed: %w", strings.Join(args, " "), err)
}

// runJSON executes kubectl with -o json and decodes the result.
func runJSON(args ...string) (map[string]any, error) {
	out, err := run(args...)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("failed to parse kubectl output as JSON: %w", err)
	}
	return data, nil
}

// runLines executes kubectl and returns its non-blank stdout lines. Paired with
// `-o name` it replaces a JSON round trip for anything that is just a list of
// names.
func runLines(args ...string) ([]string, error) {
	out, err := run(args...)
	if err != nil {
		return nil, err
	}
	var lines []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// ListContexts returns context names from the current kubeconfig.
func ListContexts() ([]string, error) {
	return runLines("config", "get-contexts", "-o", "name")
}

// ListNamespaces returns every namespace name. The API returns them sorted and
// names are unique, so there is nothing to sort or dedupe here.
func ListNamespaces() ([]string, error) {
	names, err := runLines("get", "namespaces", "-o", "name")
	for i, name := range names {
		names[i] = strings.TrimPrefix(name, "namespace/")
	}
	return names, err
}

// ListSecretKeys returns one entry per Secret data/stringData key.
func ListSecretKeys(allNamespaces bool, namespace string) ([]KeyRef, error) {
	return listKeys("secrets", allNamespaces, namespace, "data", "stringData")
}

// ListConfigMapKeys returns one entry per ConfigMap data/binaryData key.
func ListConfigMapKeys(allNamespaces bool, namespace string) ([]KeyRef, error) {
	return listKeys("configmaps", allNamespaces, namespace, "data", "binaryData")
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

func listKeys(resource string, allNamespaces bool, namespace string, fields ...string) ([]KeyRef, error) {
	args := scopeArgs([]string{"get", resource, "-o", "json"}, allNamespaces, namespace)

	data, err := runJSON(args...)
	if err != nil {
		return nil, err
	}

	var refs []KeyRef
	for _, item := range asList(data["items"]) {
		name := metaString(item, "name", "")
		if name == "" {
			continue
		}
		ns := metaString(item, "namespace", "default")

		unique := map[string]struct{}{}
		for _, field := range fields {
			for key := range asMap(item[field]) {
				unique[key] = struct{}{}
			}
		}
		for _, key := range slices.Sorted(maps.Keys(unique)) {
			refs = append(refs, KeyRef{Namespace: ns, Name: name, Key: key})
		}
	}
	return refs, nil
}

// SecretValue fetches one Secret key, base64-decoding data entries.
func SecretValue(ref KeyRef) (string, error) {
	data, err := runJSON("get", "secret", ref.Name, "-n", ref.Namespace, "-o", "json")
	if err != nil {
		return "", err
	}
	if raw, ok := asMap(data["data"])[ref.Key]; ok {
		encoded, _ := raw.(string)
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return fmt.Sprintf("<b64-decode-error> raw=%q", encoded), nil
		}
		return string(decoded), nil
	}
	if raw, ok := asMap(data["stringData"])[ref.Key]; ok {
		value, _ := raw.(string)
		return value, nil
	}
	return "<key not found>", nil
}

// ConfigMapValue fetches one ConfigMap key. Binary entries are summarized
// rather than dumped.
func ConfigMapValue(ref KeyRef) (string, error) {
	data, err := runJSON("get", "configmap", ref.Name, "-n", ref.Namespace, "-o", "json")
	if err != nil {
		return "", err
	}
	if raw, ok := asMap(data["data"])[ref.Key]; ok {
		value, _ := raw.(string)
		return value, nil
	}
	if raw, ok := asMap(data["binaryData"])[ref.Key]; ok {
		encoded, _ := raw.(string)
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

// ListPods returns pods with their containers and phase.
func ListPods(allNamespaces bool, namespace string) ([]Pod, error) {
	data, err := runJSON(scopeArgs([]string{"get", "pods", "-o", "json"}, allNamespaces, namespace)...)
	if err != nil {
		return nil, err
	}
	return parsePods(data), nil
}

func parsePods(data map[string]any) []Pod {
	var pods []Pod
	for _, item := range asList(data["items"]) {
		name := metaString(item, "name", "")
		if name == "" {
			continue
		}
		pod := Pod{
			Namespace: metaString(item, "namespace", "default"),
			Name:      name,
			Phase:     "Unknown",
		}
		if phase, ok := asMap(item["status"])["phase"].(string); ok && phase != "" {
			pod.Phase = phase
		}
		spec := asMap(item["spec"])
		for _, group := range []string{"initContainers", "containers"} {
			for _, container := range asList(spec[group]) {
				if n, ok := container["name"].(string); ok && n != "" {
					pod.Containers = append(pod.Containers, n)
				}
			}
		}
		pods = append(pods, pod)
	}
	return pods
}

// StreamLogs runs `kubectl logs` for every pod and merges the output into w a
// whole line at a time, so concurrent streams cannot interleave mid-line.
//
// kubectl's own --prefix does the labelling whenever more than one stream is in
// play; there is no reason to reimplement it here.
// tail is passed straight through to kubectl, where -1 means the whole log.
func StreamLogs(pods []Pod, follow bool, tail int, w io.Writer) error {
	lines := make(chan string, 256)
	failures := make(chan error, len(pods))
	var wg sync.WaitGroup

	for _, pod := range pods {
		args := []string{
			"logs", pod.Name, "-n", pod.Namespace,
			"--all-containers=true", "--tail=" + strconv.Itoa(tail),
		}
		if follow {
			args = append(args, "--follow")
		}
		if len(pods) > 1 || len(pod.Containers) > 1 {
			args = append(args, "--prefix")
		}

		cmd := exec.Command("kubectl", args...)
		cmd.Stderr = os.Stderr // kubectl explains its own failures
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("could not start kubectl logs for %s/%s: %w", pod.Namespace, pod.Name, err)
		}

		wg.Go(func() {
			scanner := bufio.NewScanner(stdout)
			// Log lines can be long: stack traces, embedded JSON.
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				lines <- scanner.Text()
			}
			if err := cmd.Wait(); err != nil {
				failures <- fmt.Errorf("%s/%s: %w", pod.Namespace, pod.Name, err)
			}
		})
	}

	go func() {
		wg.Wait()
		close(lines)
		close(failures)
	}()

	for line := range lines {
		fmt.Fprintln(w, line)
	}

	// kubectl already printed each reason to stderr, so only a total washout is
	// worth turning into an error.
	var errs []error
	for err := range failures {
		errs = append(errs, err)
	}
	if len(errs) == len(pods) {
		return errors.Join(errs...)
	}
	return nil
}

// UseContext switches the current kubeconfig context. kubectl's confirmation
// goes to stderr, keeping stdout free for data.
func UseContext(name string) error {
	return configure("config", "use-context", name)
}

// UseNamespace sets the default namespace on the current context.
func UseNamespace(name string) error {
	return configure("config", "set-context", "--current", "--namespace", name)
}

func configure(args ...string) error {
	out, err := run(args...)
	if len(out) > 0 {
		fmt.Fprint(os.Stderr, string(out))
	}
	return err
}

func asList(v any) []map[string]any {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func metaString(item map[string]any, field, fallback string) string {
	if s, ok := asMap(item["metadata"])[field].(string); ok && s != "" {
		return s
	}
	return fallback
}
