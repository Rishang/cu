package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Rishang/cloudutil/internal/pick"
	"github.com/Rishang/cloudutil/internal/ui"
)

// filterMode makes the fzf embedded in pick non-interactive: fzf reads
// $FZF_DEFAULT_OPTS before the args pick passes, and --filter is its headless
// mode, so a test can drive the real picker without a terminal.
func filterMode(t *testing.T, query string) *bytes.Buffer {
	t.Helper()
	t.Setenv("FZF_DEFAULT_OPTS", "--filter="+query)

	out, _ := captureUI(t)
	return out
}

// pickAndPrint backs every browse-a-secret-store command, so the flow worth
// pinning down is: only the selections are fetched, each is filed under the key
// its fetch returned, and the lot arrives as one JSON object.
func TestPickAndPrintFetchesOnlySelections(t *testing.T) {
	out := filterMode(t, "prod")

	var fetched []string
	err := pickAndPrint([]string{"prod-db", "prod-api", "stage-api"}, itself,
		"thing", "thing> ",
		func(name string) (string, string, error) {
			fetched = append(fetched, name)
			return "id/" + name, "value-of-" + name, nil
		})
	if err != nil {
		t.Fatalf("pickAndPrint: %v", err)
	}

	if len(fetched) != 2 {
		t.Fatalf("fetched %v, want only the two prod entries", fetched)
	}

	var payload map[string]string
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	want := map[string]string{
		"id/prod-db":  "value-of-prod-db",
		"id/prod-api": "value-of-prod-api",
	}
	for key, value := range want {
		if payload[key] != value {
			t.Errorf("payload[%q] = %q, want %q", key, payload[key], value)
		}
	}
	if len(payload) != len(want) {
		t.Errorf("payload has %d entries, want %d: %v", len(payload), len(want), payload)
	}
}

// A nested JSON value must stay nested rather than being escaped into a string,
// which is what cu aws secrets relies on so jq can walk into a secret.
func TestPickAndPrintNestsStructuredValues(t *testing.T) {
	out := filterMode(t, "app")

	err := pickAndPrint([]string{"app"}, itself, "secret", "secret> ",
		func(name string) (string, any, error) {
			return name, decodeSecretValue(`{"user":"admin","port":5432}`), nil
		})
	if err != nil {
		t.Fatalf("pickAndPrint: %v", err)
	}

	var payload struct {
		App struct {
			User string `json:"user"`
			Port int    `json:"port"`
		} `json:"app"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if payload.App.User != "admin" || payload.App.Port != 5432 {
		t.Errorf("got %+v, want the secret nested as an object: %s", payload.App, out.String())
	}
}

// Nothing matching is not an error, and nothing must be fetched or printed.
func TestPickAndPrintEmptySelection(t *testing.T) {
	out := filterMode(t, "nothing-matches-this")

	fetched := 0
	err := pickAndPrint([]string{"a", "b"}, itself, "thing", "thing> ",
		func(string) (string, string, error) {
			fetched++
			return "", "", nil
		})
	if err != nil {
		t.Fatalf("pickAndPrint: %v", err)
	}
	if fetched != 0 {
		t.Errorf("fetched %d items, want none", fetched)
	}
	if out.Len() != 0 {
		t.Errorf("printed %q, want nothing", out.String())
	}
}

// A fetch failure has to surface, not be swallowed into a partial payload.
func TestPickAndPrintPropagatesFetchError(t *testing.T) {
	out := filterMode(t, "b")

	sentinel := errors.New("access denied")
	err := pickAndPrint([]string{"a", "b"}, itself, "thing", "thing> ",
		func(string) (string, string, error) { return "", "", sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if out.Len() != 0 {
		t.Errorf("printed %q on failure, want nothing", out.String())
	}
}

// An empty list must not reach fzf at all; the user gets told instead.
func TestPickFromEmptyList(t *testing.T) {
	out := filterMode(t, "anything")
	errBuf := &bytes.Buffer{}
	ui.Err = errBuf

	selected, err := pickFrom(nil, itself, "AWS secret", pick.Options{})
	if err != nil {
		t.Fatalf("pickFrom: %v", err)
	}
	if selected != nil {
		t.Fatalf("selected %v, want nothing", selected)
	}
	if !strings.Contains(errBuf.String(), "No AWS secrets found") {
		t.Errorf("stderr = %q, want it to name the noun", errBuf.String())
	}
	if out.Len() != 0 {
		t.Errorf("printed %q, want nothing", out.String())
	}
}
