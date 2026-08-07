package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rishang/cloudutil/internal/ui"
)

// TestSplitPrefix pins the --path contract every backend maps onto its own
// hierarchy: the first n segments are containers, the rest is a path, and
// missing segments come back empty meaning "all of them".
func TestSplitPrefix(t *testing.T) {
	cases := []struct {
		prefix     string
		n          int
		containers []string
		rest       string
	}{
		{"", 1, []string{""}, ""},
		{"secret", 1, []string{"secret"}, ""},
		{"secret/team/db", 1, []string{"secret"}, "team/db"},
		{"/secret/team/", 1, []string{"secret"}, "team"},
		{"", 2, []string{"", ""}, ""},
		{"api", 2, []string{"api", ""}, ""},
		{"api/prod", 2, []string{"api", "prod"}, ""},
		{"api/prod/db/creds", 2, []string{"api", "prod"}, "db/creds"},
	}
	for _, tc := range cases {
		containers, rest := splitPrefix(tc.prefix, tc.n)
		if len(containers) != len(tc.containers) || rest != tc.rest {
			t.Fatalf("splitPrefix(%q, %d) = %v, %q; want %v, %q",
				tc.prefix, tc.n, containers, rest, tc.containers, tc.rest)
		}
		for i := range containers {
			if containers[i] != tc.containers[i] {
				t.Fatalf("splitPrefix(%q, %d) = %v; want %v",
					tc.prefix, tc.n, containers, tc.containers)
			}
		}
	}
}

// TestSecretProvidersConfig covers the shared config contract: several providers
// coexist in one file, and the provider field decides which backend a profile
// dials.
func TestSecretProvidersConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "cu"), 0o700); err != nil {
		t.Fatal(err)
	}
	doc := `
- profile: prod
  provider: vault
  endpoint: https://vault.example.com
  credentials:
    token: s.prod
    namespace: eng
- profile: inf-prod
  provider: infisical
  endpoint: https://us.infisical.com
  credentials:
    client_id: 8f1a
    client_secret: 4c2b
- profile: mystery
  provider: someday
  endpoint: https://example.com
`
	if err := os.WriteFile(secretProvidersPath(), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	profiles, err := loadSecretProviders()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(profiles) != 3 {
		t.Fatalf("loaded %d profiles, want 3", len(profiles))
	}

	// Named lookup, and the credential keys each provider needs.
	vaultProfile, err := resolveProfile(profiles, "prod")
	if err != nil || vaultProfile.Credentials.Token != "s.prod" ||
		vaultProfile.Credentials.Namespace != "eng" {
		t.Fatalf("resolveProfile(prod) = %+v, %v", vaultProfile, err)
	}
	infProfile, err := resolveProfile(profiles, "inf-prod")
	if err != nil || infProfile.Credentials.ClientID != "8f1a" ||
		infProfile.Credentials.ClientSecret != "4c2b" {
		t.Fatalf("resolveProfile(inf-prod) = %+v, %v", infProfile, err)
	}
	if _, err := resolveProfile(profiles, "nope"); err == nil {
		t.Fatal("want an error for an unknown profile name")
	}

	// A vault profile with a token dials without a login round-trip, so this
	// reaches no network.
	ctx := context.Background()
	if _, err := newSecretStore(ctx, vaultProfile); err != nil {
		t.Fatalf("newSecretStore(vault): %v", err)
	}
	// An unknown provider is refused by name rather than silently ignored.
	if _, err := newSecretStore(ctx, profiles[2]); err == nil {
		t.Fatal("want an error for an unknown provider")
	}
	// So is a profile missing the endpoint every backend needs.
	if _, err := newSecretStore(ctx, secretProvider{Profile: "x", Provider: "vault"}); err == nil {
		t.Fatal("want an error for a profile with no endpoint")
	}

	// The file holds plaintext tokens, so loading a world-readable one warns.
	prevErr := ui.Err
	stderr := &bytes.Buffer{}
	ui.Err = stderr
	t.Cleanup(func() { ui.Err = prevErr })
	if err := os.Chmod(secretProvidersPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSecretProviders(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(stderr.String(), "readable by other users") {
		t.Errorf("stderr = %q, want a warning about the file mode", stderr.String())
	}
}
