package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// serviceAccountDir is where the kubelet projects a pod's credentials. A
// variable so tests can point it at a fixture.
var serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// kubectlEnv returns the environment to run kubectl with, or nil to inherit
// this process's own — which is the case everywhere except inside a pod with no
// kubeconfig, where it names a synthesized one instead.
//
// This exists because kubectl, unlike the client libraries, has no in-cluster
// fallback: with no kubeconfig it fails rather than reading the token every pod
// already has mounted.
func kubectlEnv() []string {
	if path := inClusterKubeconfig(); path != "" {
		return append(os.Environ(), "KUBECONFIG="+path)
	}
	return nil
}

// inClusterKubeconfig is the path to the kubeconfig cu synthesized for a pod,
// or empty when it did not need to. Resolved once: the answer cannot change
// within one command, and writing the file again per kubectl call would be
// pointless.
var inClusterKubeconfig = sync.OnceValue(func() string {
	if !inCluster() {
		return ""
	}
	path, err := writeInClusterKubeconfig()
	if err != nil {
		// Say why cu could not supply a config, then let kubectl report the
		// missing configuration in its own words.
		fmt.Fprintf(os.Stderr, "[!] could not build an in-cluster kubeconfig: %v\n", err)
		return ""
	}
	return path
})

// inCluster reports whether cu is running in a pod with credentials mounted and
// no kubeconfig of its own. An explicit kubeconfig always wins — if one is
// mounted or configured, it was put there on purpose.
func inCluster() bool {
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" || os.Getenv("KUBECONFIG") != "" {
		return false
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".kube", "config")); err == nil {
			return false
		}
	}
	_, err := os.Stat(filepath.Join(serviceAccountDir, "token"))
	return err == nil
}

// writeInClusterKubeconfig renders a kubeconfig for the projected service
// account and returns its path.
//
// It references the token by path rather than value, so no credential is copied
// anywhere and kubectl re-reads the file — projected tokens are rotated, and a
// token pasted into a config or an argv would go stale and be visible in ps.
func writeInClusterKubeconfig() (string, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if port == "" {
		port = "443"
	}
	// An IPv6 service host arrives bare and has to be bracketed in a URL.
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}

	// The pod's own namespace is the one its RBAC most likely covers, and with
	// no context to inherit from kubectl would otherwise assume "default".
	namespace, err := os.ReadFile(filepath.Join(serviceAccountDir, "namespace"))
	if err != nil {
		namespace = []byte("default")
	}

	config := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: in-cluster
clusters:
- name: in-cluster
  cluster:
    server: https://%s:%s
    certificate-authority: %s
contexts:
- name: in-cluster
  context:
    cluster: in-cluster
    user: in-cluster
    namespace: %s
users:
- name: in-cluster
  user:
    tokenFile: %s
`, host, port,
		filepath.Join(serviceAccountDir, "ca.crt"),
		strings.TrimSpace(string(namespace)),
		filepath.Join(serviceAccountDir, "token"))

	// A fixed name inside the temp dir: the file holds only paths, is rewritten
	// every run, and so needs no cleanup and cannot accumulate.
	path := filepath.Join(os.TempDir(), "cu-in-cluster.kubeconfig")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
