package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These cases deliberately stop before awsx.LoadConfig: they freeze the local
// validation contract without creating an AWS client or needing credentials.
func TestAWSLoginRejectsInvalidLocalInputBeforeAWSConfig(t *testing.T) {
	policy := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(policy, []byte(`{"Version":"2012-10-17","Statement":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidPolicy := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalidPolicy, []byte(`{"Statement":`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "requires policy file",
			args:    []string{"aws", "login"},
			wantErr: "No policy file provided",
		},
		{
			name:    "rejects duration below one hour",
			args:    []string{"aws", "login", "--policy-file", policy, "--duration", "0"},
			wantErr: "--duration must be between 1 and 24 hours, got 0.",
		},
		{
			name:    "rejects duration above one day",
			args:    []string{"aws", "login", "--policy-file", policy, "--duration", "25"},
			wantErr: "--duration must be between 1 and 24 hours, got 25.",
		},
		{
			name:    "reports unreadable policy path",
			args:    []string{"aws", "login", "--policy-file", filepath.Join(t.TempDir(), "missing.json")},
			wantErr: "Could not read policy file",
		},
		{
			name:    "rejects malformed policy JSON",
			args:    []string{"aws", "login", "--policy-file", invalidPolicy},
			wantErr: "is not valid JSON.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCu(t, tc.args...)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (stderr: %s)", code, stderr)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr, tc.wantErr)
			}
		})
	}
}

func TestDecodeSecretValueOnlyExpandsJSONObjects(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{`{"nested":{"enabled":true}}`, map[string]any{"nested": map[string]any{"enabled": true}}},
		{`["a","b"]`, `["a","b"]`},
		{`"string"`, `"string"`},
		{`not json`, "not json"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := decodeSecretValue(tc.input)
			wantJSON, err := json.Marshal(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("decodeSecretValue(%q) = %s, want %s", tc.input, gotJSON, wantJSON)
			}
		})
	}
}

func TestPwpushConfigRoundTripUsesPrivatePermissions(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	_, stderr, code := runCu(t, "pwpush", "config",
		"--token", "token-for-local-test", "--source", "https://pwpush.example///", "--email", "user@example.test")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}

	path := filepath.Join(configRoot, "cu", "psst.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("config permissions = %o, want %o", got, want)
	}
	var stored pwpushConfig
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if want := (pwpushConfig{Token: "token-for-local-test", Source: "https://pwpush.example", Email: "user@example.test"}); stored != want {
		t.Errorf("stored config = %+v, want %+v", stored, want)
	}
	if !strings.Contains(stderr, "Saved auth config to "+path) {
		t.Errorf("stderr = %q, want saved path", stderr)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runCu(t, "pwpush", "config",
		"--token", "replacement-token", "--source", "https://pwpush.example")
	if code != 0 {
		t.Fatalf("overwrite exit code = %d, stderr: %s", code, stderr)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("overwritten config permissions = %o, want %o", got, want)
	}

	loaded, err := loadPwpushConfig()
	if err != nil {
		t.Fatal(err)
	}
	if want := (pwpushConfig{Token: "replacement-token", Source: "https://pwpush.example"}); *loaded != want {
		t.Errorf("loaded overwritten config = %+v, want %+v", *loaded, want)
	}
}

func TestPwpushConfigLoadErrorsAndAuthHeaders(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	if _, err := loadPwpushConfig(); err == nil || !strings.Contains(err.Error(), "no configuration found") {
		t.Errorf("missing config error = %v, want configuration guidance", err)
	}

	path := pwpushConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"token":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPwpushConfig(); err == nil || !strings.Contains(err.Error(), "could not parse "+path) {
		t.Errorf("malformed config error = %v, want parse error naming %s", err, path)
	}

	legacy := (&pwpushConfig{Token: "legacy-token", Email: "user@example.test"}).headers()
	if got, want := legacy["X-User-Email"], "user@example.test"; got != want {
		t.Errorf("legacy email header = %q, want %q", got, want)
	}
	if got, want := legacy["X-User-Token"], "legacy-token"; got != want {
		t.Errorf("legacy token header = %q, want %q", got, want)
	}
	if _, found := legacy["Authorization"]; found {
		t.Error("legacy headers unexpectedly include Bearer authorization")
	}

	bearer := (&pwpushConfig{Token: "bearer-token"}).headers()
	if got, want := bearer["Authorization"], "Bearer bearer-token"; got != want {
		t.Errorf("Bearer header = %q, want %q", got, want)
	}
	if _, found := bearer["X-User-Token"]; found {
		t.Error("Bearer headers unexpectedly include a legacy token")
	}
	for name, headers := range map[string]map[string]string{"legacy": legacy, "bearer": bearer} {
		if got, want := headers["Accept"], "application/json"; got != want {
			t.Errorf("%s Accept header = %q, want %q", name, got, want)
		}
	}
}

func TestPwgenValidatesAndRestrictsCharacterSet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "rejects no selected character types",
			args:    []string{"pwpush", "pwgen", "--no-symbols", "--no-uppercase", "--no-lowercase", "--no-digits"},
			wantErr: "No character types selected.",
		},
		{
			name:    "rejects non-positive length",
			args:    []string{"pwpush", "pwgen", "--length", "0"},
			wantErr: "--length must be at least 1.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCu(t, tc.args...)
			if code != 1 || !strings.Contains(stderr, tc.wantErr) {
				t.Fatalf("code = %d, stderr = %q; want code 1 and %q", code, stderr, tc.wantErr)
			}
		})
	}

	stdout, stderr, code := runCu(t, "pwpush", "pwgen", "--length", "128",
		"--no-symbols", "--no-uppercase", "--no-lowercase")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	password := strings.TrimSuffix(stdout, "\n")
	if len(password) != 128 {
		t.Fatalf("password length = %d, want 128", len(password))
	}
	if strings.Trim(password, "0123456789") != "" {
		t.Errorf("password contains a non-digit: %q", password)
	}
}

func TestReadHistoryNormalizesZshEntriesAndReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	content := strings.Join([]string{
		": 1710000000:3; git status",
		"echo plain",
		": 1710000001:8; git status",
		"   ",
		" : 1710000002:1; not-a-timestamp",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{": 1710000002:1; not-a-timestamp", "echo plain", "git status"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("readHistory() = %#v, want %#v", got, want)
	}
	if _, err := readHistory(filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "history file not found") {
		t.Errorf("missing history error = %v, want a descriptive missing-file error", err)
	}
}

func TestTaskFlagParsingPreservesTaskArgumentsWithoutExecutingTask(t *testing.T) {
	cmd := newTaskCommand()
	flags := cmd.Flags()
	if err := flags.Parse([]string{"--taskfile", "custom.yml", "--directory", "/work", "deploy", "--force", "--tag=blue"}); err != nil {
		t.Fatal(err)
	}
	if got, want := flags.Lookup("taskfile").Value.String(), "custom.yml"; got != want {
		t.Errorf("taskfile = %q, want %q", got, want)
	}
	if got, want := flags.Lookup("directory").Value.String(), "/work"; got != want {
		t.Errorf("directory = %q, want %q", got, want)
	}
	if got, want := strings.Join(flags.Args(), "\x00"), "deploy\x00--force\x00--tag=blue"; got != want {
		t.Errorf("task arguments = %q, want %q", got, want)
	}
}
