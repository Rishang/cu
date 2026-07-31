package kube

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// parsePods is the only part of the pod listing that is not kubectl's job, so
// it is the part worth pinning down.
func TestParsePods(t *testing.T) {
	const raw = `{"items": [
		{
			"metadata": {"name": "api-1", "namespace": "prod"},
			"spec": {"containers": [{"name": "app"}, {"name": "sidecar"}]},
			"status": {"phase": "Running"}
		},
		{
			"metadata": {"name": "worker-1"},
			"spec": {
				"initContainers": [{"name": "migrate"}],
				"containers": [{"name": "worker"}]
			},
			"status": {}
		},
		{"metadata": {"namespace": "prod"}, "spec": {}}
	]}`

	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}

	want := []Pod{
		{Namespace: "prod", Name: "api-1", Phase: "Running", Containers: []string{"app", "sidecar"}},
		// No namespace falls back to default, no phase to Unknown, and init
		// containers come first so --all-containers output lines up.
		{Namespace: "default", Name: "worker-1", Phase: "Unknown", Containers: []string{"migrate", "worker"}},
		// The nameless entry is dropped: kubectl logs has nothing to target.
	}
	if got := parsePods(data); !reflect.DeepEqual(got, want) {
		t.Errorf("parsePods()\n got %+v\nwant %+v", got, want)
	}
}

// stubKubectl puts a fake kubectl at the front of PATH for the duration of a
// test, emitting eight identifiable log lines.
func stubKubectl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"for i in 1 2 3 4 5 6 7 8; do printf '[%s] line ' \"$2\"; printf '%s\\n' \"$i\"; done\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestStreamLogsWritesPodOutput(t *testing.T) {
	stubKubectl(t)

	var buf bytes.Buffer
	pod := Pod{Namespace: "prod", Name: "api-1", Containers: []string{"app"}}
	if err := StreamLogs(pod, false, 8, &buf); err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 8 {
		t.Fatalf("got %d lines, want 8: %q", len(lines), buf.String())
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "[api-1] line ") {
			t.Fatalf("unexpected line %q", line)
		}
	}
}

// A single container must not get --prefix; several must.
func TestStreamLogsPrefixesOnlyMultiContainerPods(t *testing.T) {
	for _, tc := range []struct {
		name       string
		containers []string
		wantPrefix bool
	}{
		{"one container", []string{"app"}, false},
		{"sidecar", []string{"app", "envoy"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Echo the argv back so the test can assert on the flags.
			if err := os.WriteFile(filepath.Join(dir, "kubectl"),
				[]byte("#!/bin/sh\necho \"$@\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

			var buf bytes.Buffer
			pod := Pod{Namespace: "prod", Name: "api-1", Containers: tc.containers}
			if err := StreamLogs(pod, false, -1, &buf); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(buf.String(), "--prefix"); got != tc.wantPrefix {
				t.Errorf("--prefix present = %v, want %v (argv: %s)", got, tc.wantPrefix, buf.String())
			}
		})
	}
}

func TestStreamLogsReportsFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kubectl"),
		[]byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := StreamLogs(Pod{Namespace: "prod", Name: "api-1"}, false, -1, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "prod/api-1") {
		t.Errorf("got %v, want an error naming the pod", err)
	}
}

func TestPodDisplay(t *testing.T) {
	pod := Pod{Namespace: "prod", Name: "api-1", Phase: "CrashLoopBackOff"}
	if got, want := pod.Display(), "prod/api-1 (CrashLoopBackOff)"; got != want {
		t.Errorf("Display() = %q, want %q", got, want)
	}
}
