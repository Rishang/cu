package kube

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubServiceAccount stands in for the kubelet's projected volume.
func stubServiceAccount(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"token":     "eyJhbGciOi.fake",
		"ca.crt":    "-----BEGIN CERTIFICATE-----\nnot a real one\n-----END CERTIFICATE-----\n",
		"namespace": "payments\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	prev := serviceAccountDir
	serviceAccountDir = dir
	t.Cleanup(func() { serviceAccountDir = prev })

	// A kubeconfig anywhere else would take precedence, including the real one
	// belonging to whoever runs the tests.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")
	return dir
}

// TestInCluster covers the precedence rules: credentials alone are not enough,
// and any explicit kubeconfig wins.
func TestInCluster(t *testing.T) {
	dir := stubServiceAccount(t)
	if !inCluster() {
		t.Fatal("inCluster() = false with a token mounted and no kubeconfig")
	}

	// An explicit kubeconfig was put there on purpose.
	t.Setenv("KUBECONFIG", "/somewhere/config")
	if inCluster() {
		t.Error("inCluster() = true although KUBECONFIG is set")
	}
	t.Setenv("KUBECONFIG", "")

	// So was one at the default location.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".kube"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".kube", "config"), []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if inCluster() {
		t.Error("inCluster() = true although ~/.kube/config exists")
	}
	t.Setenv("HOME", t.TempDir())

	// Outside a pod there is no service host, whatever else is lying around.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	if inCluster() {
		t.Error("inCluster() = true outside a pod")
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")

	// Credentials are what make it in-cluster, so a missing token rules it out.
	if err := os.Remove(filepath.Join(dir, "token")); err != nil {
		t.Fatal(err)
	}
	if inCluster() {
		t.Error("inCluster() = true with no token file")
	}
}

// TestWriteInClusterKubeconfig checks the generated file against the real
// kubectl, which needs no cluster to parse a kubeconfig and report what it
// resolves to. It also pins that the token is referenced by path, never copied.
func TestWriteInClusterKubeconfig(t *testing.T) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not available")
	}
	dir := stubServiceAccount(t)

	path, err := writeInClusterKubeconfig()
	if err != nil {
		t.Fatalf("writeInClusterKubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", path)

	// kubectl accepts it, and resolves the pod's own namespace rather than
	// falling back to "default".
	if got := CurrentContext(); got != "in-cluster" {
		t.Errorf("CurrentContext() = %q, want in-cluster", got)
	}
	if got := CurrentNamespace(); got != "payments" {
		t.Errorf("CurrentNamespace() = %q, want the pod's namespace", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config := string(raw)
	if strings.Contains(config, "eyJhbGciOi.fake") {
		t.Error("the token value was copied into the kubeconfig; it must stay a tokenFile path")
	}
	for _, want := range []string{
		"server: https://10.96.0.1:443",
		"tokenFile: " + filepath.Join(dir, "token"),
		"certificate-authority: " + filepath.Join(dir, "ca.crt"),
	} {
		if !strings.Contains(config, want) {
			t.Errorf("kubeconfig missing %q:\n%s", want, config)
		}
	}

	// Owner-only, since it points at credentials even without holding them.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("kubeconfig mode = %04o, want 0600", mode)
	}
}

// TestWriteInClusterKubeconfigDefaults covers the two fallbacks: a missing
// namespace file and an IPv6 service host, which has to be bracketed in a URL.
func TestWriteInClusterKubeconfigDefaults(t *testing.T) {
	dir := stubServiceAccount(t)
	if err := os.Remove(filepath.Join(dir, "namespace")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", "fd00::1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	path, err := writeInClusterKubeconfig()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config := string(raw)
	if !strings.Contains(config, "server: https://[fd00::1]:443") {
		t.Errorf("want a bracketed IPv6 host and the default port:\n%s", config)
	}
	if !strings.Contains(config, "namespace: default") {
		t.Errorf("want the default namespace with no namespace file:\n%s", config)
	}
}
