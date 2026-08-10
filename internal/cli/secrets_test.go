package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
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
vault:
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
	if err := os.WriteFile(configPath(), []byte(doc), 0o600); err != nil {
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
	if err := os.Chmod(configPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSecretProviders(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(stderr.String(), "readable by other users") {
		t.Errorf("stderr = %q, want a warning about the file mode", stderr.String())
	}
}

// TestSecretProfileFromEnv covers the precedence rule: VAULT_<PROVIDER>_* beats
// the file, and VAULT_PROVIDER alone replaces it.
func TestSecretProfileFromEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "cu"), 0o700); err != nil {
		t.Fatal(err)
	}
	doc := `
vault:
- profile: prod
  provider: vault
  endpoint: https://file.example.com
  credentials:
    token: s.file
    namespace: eng
`
	if err := os.WriteFile(configPath(), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	// No file needed: the environment names the provider and everything it takes.
	t.Setenv("VAULT_PROVIDER", "infisical")
	t.Setenv("VAULT_INFISICAL_ENDPOINT", "https://us.infisical.com")
	t.Setenv("VAULT_INFISICAL_CLIENT_ID", "8f1a")
	t.Setenv("VAULT_INFISICAL_CLIENT_SECRET", "4c2b")
	got, err := resolveSecretProfile("")
	if err != nil {
		t.Fatalf("resolveSecretProfile(env-only): %v", err)
	}
	if got.Provider != "infisical" || got.Endpoint != "https://us.infisical.com" ||
		got.Credentials.ClientID != "8f1a" || got.Credentials.ClientSecret != "4c2b" {
		t.Fatalf("env-only profile = %+v; want the VAULT_INFISICAL_* values, not the file's", got)
	}
	// The other block's vars are inert, so a stale export cannot leak across.
	t.Setenv("VAULT_HASHICORP_TOKEN", "s.wrong-block")
	if got, _ := resolveSecretProfile(""); got.Credentials.Token != "" {
		t.Errorf("token = %q; want empty — VAULT_HASHICORP_* must not reach an infisical profile",
			got.Credentials.Token)
	}

	// A named profile that disagrees with VAULT_PROVIDER is refused rather than
	// dialled with half of each block.
	if _, err := resolveSecretProfile("prod"); err == nil {
		t.Fatal("want an error for a hashicorp profile under VAULT_PROVIDER=infisical")
	}

	// Naming a profile brings the file back, with the environment overriding the
	// fields it sets and leaving the rest (namespace) alone.
	t.Setenv("VAULT_PROVIDER", "")
	t.Setenv("VAULT_HASHICORP_TOKEN", "s.env")
	t.Setenv("VAULT_HASHICORP_USERNAME", "ci")
	got, err = resolveSecretProfile("prod")
	if err != nil {
		t.Fatalf("resolveSecretProfile(prod): %v", err)
	}
	if got.Provider != "hashicorp" || got.Credentials.Token != "s.env" ||
		got.Credentials.Username != "ci" ||
		got.Credentials.Namespace != "eng" || got.Endpoint != "https://file.example.com" {
		t.Fatalf("overlaid profile = %+v; want the env token over the file's, everything else from the file", got)
	}
	// The profile name is not overlaid — it is how this profile was found.
	t.Setenv("VAULT_HASHICORP_PROFILE", "elsewhere")
	if got, _ := resolveSecretProfile("prod"); got.Profile != "prod" {
		t.Errorf("profile = %q; want prod", got.Profile)
	}
}

// TestMTLSClient pins httpClientFor's mTLS contract: no cert/CA/serverName
// configured means "use the shared httpClient", and a half-set or unreadable
// cert is an error rather than a silently plaintext connection.
func TestMTLSClient(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.pem")
	os.WriteFile(certPath, []byte("not a real cert"), 0o600)

	cases := []struct {
		name    string
		creds   func() (clientCert, clientKey, caCert string)
		wantErr bool
	}{
		{"no mTLS fields set", func() (string, string, string) { return "", "", "" }, false},
		{"client_cert without client_key", func() (string, string, string) { return certPath, "", "" }, true},
		{"unparsable client_cert", func() (string, string, string) { return certPath, certPath, "" }, true},
		{"missing ca_cert", func() (string, string, string) { return "", "", filepath.Join(dir, "missing.pem") }, true},
		{"non-PEM ca_cert", func() (string, string, string) { return "", "", certPath }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := secretProvider{}
			p.Credentials.ClientCert, p.Credentials.ClientKey, p.Credentials.CACert = tc.creds()
			client, err := httpClientFor(p, "")
			if tc.wantErr {
				if err == nil {
					t.Fatal("httpClientFor(): want error, got nil")
				}
				return
			}
			if err != nil || client != nil {
				t.Fatalf("httpClientFor() = %v, %v; want nil, nil", client, err)
			}
		})
	}
}

// TestEnvKeysCoverCredentials pins the tag-derived naming: every credential
// field is reachable from the environment, so adding one to secretProvider
// cannot silently leave it file-only.
func TestEnvKeysCoverCredentials(t *testing.T) {
	creds := reflect.TypeOf(secretProvider{}.Credentials)
	for i := range creds.NumField() {
		tag, _, _ := strings.Cut(creds.Field(i).Tag.Get("yaml"), ",")
		if tag == "" {
			t.Fatalf("%s has no yaml tag, so it has no environment variable either",
				creds.Field(i).Name)
		}
		t.Setenv("VAULT_HASHICORP_"+strings.ToUpper(tag), "set")
		overlaid := envOverlay(secretProvider{Provider: "vault"})
		got := reflect.ValueOf(overlaid.Credentials).Field(i).String()
		if got != "set" {
			t.Errorf("credentials.%s = %q after VAULT_HASHICORP_%s=set; want it applied",
				tag, got, strings.ToUpper(tag))
		}
	}
}
