package cli

import "testing"

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
