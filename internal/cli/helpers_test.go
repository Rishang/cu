package cli

import (
	"os"
	"strings"
	"testing"
)

// TestExpandConfigEnv pins the ${VAR} contract: braced references expand,
// unset ones go empty, and anything else (bare $VAR, $$) is left alone so a
// secret containing a literal "$" survives.
func TestExpandConfigEnv(t *testing.T) {
	t.Setenv("CU_TEST_TOKEN", "s.abc123")
	cases := []struct {
		in, want string
	}{
		{"token: ${CU_TEST_TOKEN}", "token: s.abc123"},
		{"token: ${CU_TEST_UNSET}", "token: "},
		{"token: $CU_TEST_TOKEN", "token: $CU_TEST_TOKEN"},
		{"token: p$$w0rd", "token: p$$w0rd"},
	}
	for _, tc := range cases {
		if got := string(expandConfigEnv([]byte(tc.in))); got != tc.want {
			t.Errorf("expandConfigEnv(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// cu pwpush config rewrites only the psst section of config.yml. Everything
// else must survive as the user wrote it: a ${VAR} reference, or the secret it
// points at lands in the file in plaintext; and their comments.
func TestPwpushConfigRewritesOnlyItsSection(t *testing.T) {
	cases := []struct {
		name     string
		original string // "" means no config.yml yet
	}{
		{"replaces an existing psst section", `# team secrets, see wiki
psst:
  token: old-token # rotate quarterly
  source: https://old.example.com
vault:
  - profile: prod
    provider: vault
    endpoint: https://vault.example.com
    credentials:
      token: ${CU_TEST_VAULT_TOKEN} # from 1password
      password: ${CU_TEST_UNSET}
`},
		{"appends psst when the file has none", `# team secrets, see wiki
vault:
  - profile: prod
    provider: vault
    endpoint: https://vault.example.com
    credentials:
      token: ${CU_TEST_VAULT_TOKEN} # from 1password
      password: ${CU_TEST_UNSET}`},
		{"creates the file when there is none", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", home)
			t.Setenv("CU_TEST_VAULT_TOKEN", "s3cret-value")
			t.Setenv("CU_TEST_UNSET", "")
			if tc.original != "" {
				if err := os.MkdirAll(configHome(), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(configPath(), []byte(tc.original), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if _, stderr, code := runCu(t, "pwpush", "config",
				"--token", "pw-token", "--source", "https://pwpush.example.com/"); code != 0 {
				t.Fatalf("pwpush config exited %d: %s", code, stderr)
			}

			saved, err := os.ReadFile(configPath())
			if err != nil {
				t.Fatal(err)
			}
			got := string(saved)
			if strings.Contains(got, "s3cret-value") {
				t.Errorf("the expanded Vault token was written to config.yml in plaintext:\n%s", got)
			}
			if strings.Contains(got, "old-token") {
				t.Errorf("the old Password Pusher token survived; the section was not replaced:\n%s", got)
			}
			// Load it back the way every command does, so a malformed rewrite
			// fails here rather than on the user's next command.
			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("rewritten config.yml no longer loads: %v\n%s", err, got)
			}
			if cfg.Psst.Token != "pw-token" || cfg.Psst.Source != "https://pwpush.example.com" {
				t.Errorf("psst = %+v, want the new token and source (trailing slash trimmed)", cfg.Psst)
			}
			if tc.original == "" {
				return
			}
			for _, keep := range []string{"${CU_TEST_VAULT_TOKEN}", "${CU_TEST_UNSET}",
				"# team secrets, see wiki", "# from 1password"} {
				if !strings.Contains(got, keep) {
					t.Errorf("%q was lost on save, so the user's config silently changed:\n%s", keep, got)
				}
			}
			if len(cfg.Vault) != 1 || cfg.Vault[0].Credentials.Token != "s3cret-value" {
				t.Errorf("vault section no longer resolves: %+v", cfg.Vault)
			}
		})
	}
}
