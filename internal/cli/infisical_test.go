package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// calls records the API calls a fake server saw. Scopes are listed
// concurrently, so the handler runs on several goroutines at once.
type calls struct {
	mu   sync.Mutex
	seen []string
}

func (c *calls) add(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, call)
}

func (c *calls) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.seen...)
}

func (c *calls) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = nil
}

// infisicalServer stands in for the API: Universal Auth login, one project
// listing with two environments, and secrets per project/environment scope.
// "locked" is a project the identity may see but not read secrets from.
func infisicalServer(t *testing.T, seen *calls) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			body, _ := io.ReadAll(r.Body)
			var login map[string]string
			if err := json.Unmarshal(body, &login); err != nil {
				t.Errorf("login body: %v", err)
			}
			if login["clientId"] != "8f1a" || login["clientSecret"] != "4c2b" {
				t.Errorf("login sent %v, want the configured client id and secret", login)
			}
			if _, sent := login["organizationSlug"]; sent {
				t.Error("organizationSlug sent although the profile has no namespace")
			}
			w.Write([]byte(`{"accessToken":"at.xyz","expiresIn":7200,"tokenType":"Bearer"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer at.xyz" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/api/v1/projects":
			w.Write([]byte(`{"projects":[
				{"id":"p1","name":"Orders Service","slug":"orders-service","environments":[
					{"name":"Development","slug":"dev"},{"name":"Production","slug":"prod"}]},
				{"id":"p2","name":"Locked","slug":"locked","environments":[
					{"name":"Production","slug":"prod"}]}]}`))
		case "/api/v4/secrets":
			query := r.URL.Query()
			seen.add(query.Get("projectId") + "/" + query.Get("environment") +
				" path=" + query.Get("secretPath") + " recursive=" + query.Get("recursive"))
			if query.Get("projectId") == "p2" {
				// Visible in the project list, unreadable here.
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Write([]byte(`{"secrets":[
				{"secretKey":"API_KEY","secretValue":"abc","secretPath":"/"},
				{"secretKey":"DB_PASSWORD","secretValue":"hunter2","secretPath":"/db"},
				{"secretKey":"CONFIG","secretValue":"{\"a\":1}","secretPath":"/db"}],
				"imports":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestInfisicalListAllScopes covers the default path: Universal Auth login,
// project discovery, one recursive call per project/environment pair, a
// forbidden project skipped, and the display path shape.
func TestInfisicalListAllScopes(t *testing.T) {
	seen := &calls{}
	server := infisicalServer(t, seen)
	defer server.Close()

	profile := secretProvider{Provider: "infisical", Endpoint: server.URL}
	profile.Credentials.ClientID = "8f1a"
	profile.Credentials.ClientSecret = "4c2b"

	ctx := context.Background()
	store, err := newInfisicalClient(ctx, profile)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token := store.(*infisicalClient).token; token != "at.xyz" {
		t.Fatalf("token = %q, want the one login returned", token)
	}

	secrets, err := store.list(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// Two readable scopes x three secrets; the third project/env 403s.
	if len(secrets) != 6 {
		t.Fatalf("list returned %d secrets, want 6: %v", len(secrets), display(secrets))
	}
	if made := seen.all(); len(made) != 3 {
		t.Fatalf("made %d secret calls, want 3 (one per scope): %v", len(made), made)
	}
	for _, call := range seen.all() {
		if !strings.Contains(call, "recursive=true") {
			t.Fatalf("call %q is not recursive — it would miss subfolders", call)
		}
	}

	// project/env/folder/KEY, with the root folder contributing no segment.
	want := map[string]bool{
		"orders-service/dev/API_KEY":        true,
		"orders-service/dev/db/DB_PASSWORD": true,
		"orders-service/prod/API_KEY":       true,
	}
	for _, path := range display(secrets) {
		delete(want, path)
	}
	if len(want) > 0 {
		t.Fatalf("missing display paths %v in %v", want, display(secrets))
	}

	// Values come from the list call, so no extra request, and a JSON object
	// value is nested like every other provider's.
	before := len(seen.all())
	for _, ref := range secrets {
		value, err := ref.get(ctx)
		if err != nil {
			t.Fatalf("get %s: %v", ref.Path, err)
		}
		if strings.HasSuffix(ref.Path, "/CONFIG") {
			nested, isObject := value.(map[string]any)
			if !isObject || nested["a"] != float64(1) {
				t.Fatalf("CONFIG = %#v, want a nested object", value)
			}
		}
	}
	if after := len(seen.all()); after != before {
		t.Fatalf("reading values made %d extra calls, want 0", after-before)
	}
}

// TestInfisicalListPrefixScopes checks that --path narrows server-side: its
// first two segments pick the project and environment, the rest becomes
// secretPath, so only the one scope is queried.
func TestInfisicalListPrefixScopes(t *testing.T) {
	seen := &calls{}
	server := infisicalServer(t, seen)
	defer server.Close()

	profile := secretProvider{Provider: "infisical", Endpoint: server.URL}
	profile.Credentials.Token = "at.xyz"
	store, err := newInfisicalClient(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.list(context.Background(), "orders-service/prod/db"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if made := seen.all(); len(made) != 1 || made[0] != "p1/prod path=/db recursive=true" {
		t.Fatalf("calls = %v, want just p1/prod at /db", made)
	}

	// A project name or id works as well as its slug, since which one a user
	// has to hand depends on where they copied it from.
	for _, name := range []string{"Orders Service", "p1"} {
		seen.reset()
		if _, err := store.list(context.Background(), name+"/dev"); err != nil {
			t.Fatalf("list(%q): %v", name, err)
		}
		if made := seen.all(); len(made) != 1 || !strings.HasPrefix(made[0], "p1/dev") {
			t.Fatalf("list(%q) called %v, want p1/dev", name, made)
		}
	}

	// An unmatched prefix is an error, not an empty list — a typo should say so.
	if _, err := store.list(context.Background(), "nope/dev"); err == nil {
		t.Fatal("want an error for a project that does not exist")
	}
}

func display(refs []secretRef) []string {
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		paths = append(paths, ref.Display())
	}
	return paths
}
