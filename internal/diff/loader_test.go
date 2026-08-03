package diff

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// asset returns the path to a shared test fixture.
func asset(name string) string {
	return filepath.Join("..", "..", "tests", "assets", name)
}

// write drops content into a temp file and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestLoadFileJSON(t *testing.T) {
	t.Run("scalars", func(t *testing.T) {
		got, err := LoadFile(write(t, "a.json", `{"key": "value", "num": 42}`))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		want := map[string]any{"key": "value", "num": int64(42)}
		if !reflect.DeepEqual(normalize(got), want) {
			t.Fatalf("got %#v, want %#v", normalize(got), want)
		}
	})

	t.Run("nested", func(t *testing.T) {
		got, err := LoadFile(write(t, "a.json", `{"a": {"b": [1, 2, 3]}}`))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		nested := normalize(got).(map[string]any)["a"].(map[string]any)["b"]
		if !reflect.DeepEqual(nested, []any{int64(1), int64(2), int64(3)}) {
			t.Fatalf("got %#v", nested)
		}
	})

	t.Run("empty object", func(t *testing.T) {
		got, err := LoadFile(write(t, "empty.json", `{}`))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		if !reflect.DeepEqual(got, map[string]any{}) {
			t.Fatalf("got %#v, want empty map", got)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := LoadFile(write(t, "bad.json", `{not valid json}`))
		assertErrContains(t, err, "Invalid JSON")
	})
}

func TestLoadFileYAML(t *testing.T) {
	t.Run("scalars", func(t *testing.T) {
		got, err := LoadFile(write(t, "a.yaml", "key: value\nnum: 42\n"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		want := map[string]any{"key": "value", "num": int64(42)}
		if !reflect.DeepEqual(normalize(got), want) {
			t.Fatalf("got %#v, want %#v", normalize(got), want)
		}
	})

	t.Run("yml extension", func(t *testing.T) {
		got, err := LoadFile(write(t, "a.yml", "x: 1\n"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		if !reflect.DeepEqual(normalize(got), map[string]any{"x": int64(1)}) {
			t.Fatalf("got %#v", normalize(got))
		}
	})

	t.Run("nested", func(t *testing.T) {
		got, err := LoadFile(write(t, "n.yaml", "spec:\n  replicas: 3\n  image: nginx\n"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec := normalize(got).(map[string]any)["spec"].(map[string]any)
		if spec["replicas"] != int64(3) {
			t.Fatalf("replicas = %v (%T)", spec["replicas"], spec["replicas"])
		}
	})

	t.Run("empty file is nil", func(t *testing.T) {
		got, err := LoadFile(write(t, "empty.yaml", ""))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		if got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})

	t.Run("null forms", func(t *testing.T) {
		got, err := LoadFile(write(t, "nulls.yaml", "key: null\nother: ~\n"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		m := normalize(got).(map[string]any)
		if m["key"] != nil || m["other"] != nil {
			t.Fatalf("got %#v, want both nil", m)
		}
	})

	t.Run("list at root", func(t *testing.T) {
		got, err := LoadFile(write(t, "list.yaml", "- a\n- b\n- c\n"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		if !reflect.DeepEqual(normalize(got), []any{"a", "b", "c"}) {
			t.Fatalf("got %#v", normalize(got))
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := LoadFile(write(t, "bad.yaml", "key: [\n  - unbalanced\n"))
		assertErrContains(t, err, "Invalid YAML")
	})
}

func TestLoadFileTOML(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		got, err := LoadFile(write(t, "a.toml",
			"[server]\nhost = \"localhost\"\nport = 8080\n"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		server := normalize(got).(map[string]any)["server"].(map[string]any)
		if server["host"] != "localhost" {
			t.Errorf("host = %v", server["host"])
		}
		if server["port"] != int64(8080) {
			t.Errorf("port = %v (%T)", server["port"], server["port"])
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := LoadFile(write(t, "bad.toml", "key = [invalid toml"))
		assertErrContains(t, err, "Invalid TOML")
	})
}

func TestLoadFileErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := LoadFile("/nonexistent/path/file.yaml")
		assertErrContains(t, err, "File not found")
	})

	t.Run("unsupported extension", func(t *testing.T) {
		_, err := LoadFile(write(t, "config.xml", "<root/>"))
		assertErrContains(t, err, "Unsupported format")
	})
}

func TestIsHCL(t *testing.T) {
	cases := map[string]bool{
		"main.tf":       true,
		"vars.tfvars":   true,
		"config.hcl":    true,
		"UPPER.TF":      true,
		"values.yaml":   false,
		"config.json":   false,
		"pyproject.tml": false,
	}
	for path, want := range cases {
		if got := IsHCL(path); got != want {
			t.Errorf("IsHCL(%q) = %v, want %v", path, got, want)
		}
	}
}

// GitBranch is best-effort: it must never fail the diff, only omit the label.
func TestGitBranchOutsideRepo(t *testing.T) {
	if got := GitBranch(filepath.Join(t.TempDir(), "file.yaml")); got != "" {
		t.Logf("GitBranch outside a repo returned %q (a parent dir is a repo)", got)
	}
}

func TestGitBranchInsideRepo(t *testing.T) {
	branch := GitBranch(asset("app-dev.yaml"))
	if branch == "" {
		t.Skip("not running inside a git checkout")
	}
	if strings.TrimSpace(branch) != branch {
		t.Errorf("branch %q has surrounding whitespace", branch)
	}
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}
