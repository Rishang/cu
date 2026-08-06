package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestVaultWalkAndRead covers the parts with real logic: userpass login, the
// recursive KV v2 LIST walk, namespace/token headers, and reading a value.
func TestVaultWalkAndRead(t *testing.T) {
	// Folders of one level are listed concurrently, so the handler runs on
	// several goroutines at once.
	var mu sync.Mutex
	var seenNamespace string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenNamespace = r.Header.Get("X-Vault-Namespace")
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/auth/userpass/login/bob":
			w.Write([]byte(`{"auth":{"client_token":"s.abc"}}`))
			return
		}
		if r.Header.Get("X-Vault-Token") != "s.abc" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/v1/sys/internal/ui/mounts":
			w.Write([]byte(`{"data":{"secret":{
				"secret/":{"type":"kv","options":{"version":"2"}},
				"legacy/":{"type":"kv","options":{"version":"1"}},
				"cubbyhole/":{"type":"cubbyhole"}}}}`))
		case "/v1/secret/metadata":
			w.Write([]byte(`{"data":{"keys":["top","team/","locked/"]}}`))
		case "/v1/secret/metadata/team":
			w.Write([]byte(`{"data":{"keys":["db"]}}`))
		case "/v1/secret/metadata/locked":
			// A subtree this token may see listed but not descend into.
			w.WriteHeader(http.StatusForbidden)
		case "/v1/secret/data/team/db":
			w.Write([]byte(`{"data":{"data":{"password":"hunter2","json":"{\"a\":1}"}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	profile := secretProvider{Provider: "vault", Endpoint: server.URL}
	profile.Credentials.Username = "bob"
	profile.Credentials.Password = "pw"
	profile.Credentials.Namespace = "default"

	ctx := context.Background()
	store, err := newVaultClient(ctx, profile)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token := store.(*vaultClient).token; token != "s.abc" {
		t.Fatalf("token = %q, want s.abc", token)
	}

	// Mount discovery keeps only KV v2, and locked/ 403s mid-walk and must be
	// skipped rather than failing the run.
	secrets, err := store.list(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(secrets) != 2 ||
		secrets[0].Display() != "secret/top" || secrets[1].Display() != "secret/team/db" {
		t.Fatalf("list = %v, want [secret/top secret/team/db]", secrets)
	}

	raw, err := secrets[1].get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	value, isMap := raw.(map[string]any)
	if !isMap || value["password"] != "hunter2" {
		t.Fatalf("value = %#v, want password=hunter2", raw)
	}
	// A JSON-object value is nested, like AWS and Azure secrets.
	nested, isObject := value["json"].(map[string]any)
	if !isObject || nested["a"] != float64(1) {
		t.Fatalf("value[json] = %#v, want a nested object", value["json"])
	}
	if seenNamespace != "default" {
		t.Fatalf("namespace header = %q, want default", seenNamespace)
	}
}

// TestVaultListPrefixNamesTheMount checks the --path contract: its first segment
// is the mount, the rest is a path inside it, and naming one skips discovery.
func TestVaultListPrefixNamesTheMount(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/v1/kv/metadata/team/db" {
			w.Write([]byte(`{"data":{"keys":["password"]}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	profile := secretProvider{Provider: "vault", Endpoint: server.URL}
	profile.Credentials.Token = "s.abc"
	store, err := newVaultClient(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}

	secrets, err := store.list(context.Background(), "kv/team/db")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(secrets) != 1 || secrets[0].Display() != "kv/team/db/password" {
		t.Fatalf("list = %v, want [kv/team/db/password]", secrets)
	}
	for _, path := range paths {
		if path == "/v1/sys/internal/ui/mounts" {
			t.Fatal("named a mount in --path but discovery ran anyway")
		}
	}
}

// TestVaultWalkListsALevelConcurrently is the check that the fan-out is real:
// the folders of one level must be listed together, not one after another.
func TestVaultWalkListsALevelConcurrently(t *testing.T) {
	const folders = 4

	var (
		mu                 sync.Mutex
		inFlight, maxSeen  int
		arrived            = make(chan struct{}, folders)
		releaseWhenAllHere = make(chan struct{})
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/secret/metadata" {
			w.Write([]byte(`{"data":{"keys":["a/","b/","c/","d/"]}}`))
			return
		}

		mu.Lock()
		inFlight++
		maxSeen = max(maxSeen, inFlight)
		mu.Unlock()
		// Block until every folder of this level is in flight, so a sequential
		// walk fails the assertion instead of racing past it.
		arrived <- struct{}{}
		if len(arrived) == folders {
			close(releaseWhenAllHere)
		}
		select {
		case <-releaseWhenAllHere:
		case <-time.After(2 * time.Second):
		}
		mu.Lock()
		inFlight--
		mu.Unlock()

		w.Write([]byte(`{"data":{"keys":["leaf"]}}`))
	}))
	defer server.Close()

	profile := secretProvider{Provider: "vault", Endpoint: server.URL}
	profile.Credentials.Token = "s.abc"
	store, err := newVaultClient(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}

	secrets, err := store.list(context.Background(), "secret")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(secrets) != folders {
		t.Fatalf("list found %d secrets, want %d", len(secrets), folders)
	}
	if maxSeen < folders {
		t.Fatalf("max concurrent LIST calls = %d, want %d — the level is not fanning out",
			maxSeen, folders)
	}
}
