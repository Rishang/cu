package awsx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSSORegion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	config := `[profile sso-session]
sso_session = company
region = us-west-2

[sso-session company]
sso_region = ap-south-1
sso_start_url = https://example.awsapps.com/start

[profile legacy-sso]
sso_region = eu-central-1
sso_start_url = https://example.awsapps.com/start
region = us-west-2
sso_account_id = 123456789012
sso_role_name = ReadOnly

[profile regular]
region = eu-west-1
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONFIG_FILE", configPath)
	// Keep the user's real ~/.aws/credentials out of the test.
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	cases := []struct {
		name    string
		profile string
		want    string
	}{
		{"sso-session region beats profile region", "sso-session", "ap-south-1"},
		{"legacy per-profile sso_region is used", "legacy-sso", "eu-central-1"},
		{"non-SSO profile has no SSO region", "regular", ""},
		{"missing default profile is not an error", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(context.Background(), tc.profile, "")
			if err != nil {
				t.Fatalf("LoadConfig(%q) failed, so cu aws login would abort for this setup: %v", tc.profile, err)
			}
			if got := SSORegion(cfg); got != tc.want {
				t.Errorf("SSORegion for profile %q = %q, want %q; the console URL would open in the wrong region", tc.profile, got, tc.want)
			}
		})
	}
}
