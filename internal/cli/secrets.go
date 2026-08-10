package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rishang/cloudutil/internal/pick"
	"github.com/Rishang/cloudutil/internal/ui"
)

// secretProvider is one entry in the "vault" list of ~/.config/cu/config.yml.
// Entries are keyed by provider so a single command can browse several
// backends; see the README for the shape. Credentials carry whichever of the
// fields the provider actually supports.
type secretProvider struct {
	Profile     string `yaml:"profile"`
	Provider    string `yaml:"provider"`
	Endpoint    string `yaml:"endpoint"`
	Credentials struct {
		Token    string `yaml:"token"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
		// ClientID and ClientSecret are Infisical's Universal Auth pair.
		ClientID     string `yaml:"client_id"`
		ClientSecret string `yaml:"client_secret"`
		// Namespace is a Vault Enterprise namespace, or Infisical's
		// organizationSlug — both name the tenant the credentials belong to.
		Namespace string `yaml:"namespace"`
		// ClientCert, ClientKey and CACert are all optional and enable mTLS:
		// ClientCert/ClientKey are a PEM pair presented to the server, CACert
		// a PEM bundle to verify the server against, for a private CA. Plain
		// TLS (no client cert, system CA pool) works with none of them set.
		ClientCert string `yaml:"client_cert"`
		ClientKey  string `yaml:"client_key"`
		CACert     string `yaml:"ca_cert"`
	} `yaml:"credentials"`
}

// loadSecretProviders reads the "vault" profile list from config.yml.
func loadSecretProviders() ([]secretProvider, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if len(cfg.Vault) == 0 {
		return nil, fmt.Errorf("no vault profiles configured — add a \"vault:\" list to %s", configPath())
	}
	return cfg.Vault, nil
}

// resolveProfile picks the named profile, the only one, or asks.
func resolveProfile(profiles []secretProvider, name string) (secretProvider, error) {
	if name != "" {
		for _, profile := range profiles {
			if profile.Profile == name {
				return profile, nil
			}
		}
		return secretProvider{}, fmt.Errorf("no profile %q in %s", name, configPath())
	}
	if len(profiles) == 1 {
		return profiles[0], nil
	}

	// The provider is on the fzf line: profiles are usually named for the
	// environment, not the backend they live in.
	selected, err := pickFrom(profiles, func(p secretProvider) string {
		return p.Profile + " (" + p.Provider + ")"
	}, "profile", pick.Options{Prompt: "profile> "})
	if err != nil {
		return secretProvider{}, err
	}
	if len(selected) == 0 {
		return secretProvider{}, exitWith(1)
	}
	return selected[0], nil
}

// secretRef is one secret in whatever backend it came from. Path doubles as the
// fzf line and the output key, so it always reads outermost-container-first:
// mount/path for Vault, project/env/folder/KEY for Infisical.
type secretRef struct {
	Path string
	// get fetches the value. Backends whose list call already returned it hand
	// back a closure over that value rather than making a second request.
	get func(context.Context) (any, error)
}

// Display names the fzf line, matching kube.KeyRef.Display.
func (r secretRef) Display() string { return r.Path }

// secretStore is a browsable secret backend. One method, because listing is the
// only thing the two providers do the same way — reading is folded into the refs
// that list returns.
type secretStore interface {
	// list returns every secret whose Display starts with prefix. An empty
	// prefix means everything the credentials can see.
	list(ctx context.Context, prefix string) ([]secretRef, error)
}

// canonicalProvider folds a provider name onto the spelling the env vars and the
// dial switch use. "vault" was the original name for the HashiCorp backend, so
// existing config files keep working.
func canonicalProvider(name string) string {
	name = strings.ToLower(name)
	if name == "vault" {
		return "hashicorp"
	}
	return name
}

// envOverlay layers VAULT_* over a profile, keyed by the profile's own
// provider: VAULT_HASHICORP_TOKEN, VAULT_INFISICAL_CLIENT_ID and so on. The
// environment wins over the file because an export is scoped to this
// invocation while the file is the standing default — that is what lets a
// container or CI job override one credential without rewriting the config.
//
// Each key is the field's yaml tag, uppercased, so a field added to
// secretProvider is settable from the environment without touching this code.
func envOverlay(p secretProvider) secretProvider {
	p.Provider = canonicalProvider(p.Provider)
	if p.Provider == "" {
		return p
	}
	applyEnv(reflect.ValueOf(&p).Elem(), "VAULT_"+strings.ToUpper(p.Provider)+"_")
	return p
}

// applyEnv sets each string field of v from prefix + its uppercased yaml tag,
// flattening nested structs onto the same prefix so credentials.token reads
// VAULT_<PROVIDER>_TOKEN. Empty and unset mean the same thing: fall through to
// whatever the file had.
func applyEnv(v reflect.Value, prefix string) {
	t := v.Type()
	for i := range t.NumField() {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		field := v.Field(i)
		switch {
		case field.Kind() == reflect.Struct:
			applyEnv(field, prefix)
		// profile and provider are how the profile was chosen in the first
		// place; letting a VAULT_<PROVIDER>_PROVIDER re-point it mid-overlay
		// would just be a way to disagree with the prefix already in hand.
		case tag == "" || tag == "profile" || tag == "provider":
		case field.Kind() == reflect.String && field.CanSet():
			if value := os.Getenv(prefix + strings.ToUpper(tag)); value != "" {
				field.SetString(value)
			}
		}
	}
}

// resolveSecretProfile picks the profile a command runs against. A bare
// VAULT_PROVIDER export is a whole connection on its own, so it stands in for
// the config file rather than being merged into a profile nobody asked for;
// once --profile or $VAULT_PROFILE names one, the environment overrides that
// profile's fields but not which backend it is.
func resolveSecretProfile(name string) (secretProvider, error) {
	envProvider := canonicalProvider(os.Getenv("VAULT_PROVIDER"))
	if name == "" && envProvider != "" {
		return envOverlay(secretProvider{Profile: "env", Provider: envProvider}), nil
	}
	profiles, err := loadSecretProviders()
	if err != nil {
		return secretProvider{}, err
	}
	selected, err := resolveProfile(profiles, name)
	if err != nil {
		return secretProvider{}, err
	}
	// Re-pointing a named profile at the other backend would keep the file's
	// endpoint and credentials and read the wrong VAULT_* block over them,
	// which can only end in a puzzling 401. Name the disagreement instead.
	if envProvider != "" && envProvider != canonicalProvider(selected.Provider) {
		return secretProvider{}, fmt.Errorf(
			"profile %q is %s but $VAULT_PROVIDER is %s — unset one of them",
			selected.Profile, selected.Provider, envProvider)
	}
	return envOverlay(selected), nil
}

// newSecretStore dials the backend a profile names.
func newSecretStore(ctx context.Context, profile secretProvider) (secretStore, error) {
	if profile.Endpoint == "" {
		return nil, fmt.Errorf("profile %q has no endpoint", profile.Profile)
	}
	switch canonicalProvider(profile.Provider) {
	case "hashicorp":
		return newVaultClient(ctx, profile)
	case "infisical":
		return newInfisicalClient(ctx, profile)
	case "":
		return nil, fmt.Errorf("profile %q has no provider", profile.Profile)
	default:
		return nil, fmt.Errorf("profile %q has unknown provider %q (want hashicorp or infisical)",
			profile.Profile, profile.Provider)
	}
}

// listConcurrency bounds in-flight list calls per backend. httpClient keeps an
// idle connection for each one, so going wider mostly buys new sockets, and
// Infisical's cloud rate limits start at 120 secret reads a minute.
const listConcurrency = 8

func newVaultCommand() *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Browse secret providers — HashiCorp Vault and Infisical",
		Long: "Browse the secret backends listed under \"vault:\" in " + configPath() + ".\n\n" +
			"Each profile there names its provider, so one command covers both. With no\n" +
			"--profile and more than one configured, fzf asks which.\n\n" +
			"$VAULT_PROVIDER (hashicorp or infisical) configures a connection without the\n" +
			"file at all, from VAULT_<PROVIDER>_ENDPOINT, _TOKEN, _USERNAME, _PASSWORD,\n" +
			"_CLIENT_ID, _CLIENT_SECRET and _NAMESPACE. Those override the matching fields\n" +
			"of a named profile too.",
	}
	// Persistent so both `cu vault -p x secrets` and `cu vault secrets -p x` work.
	// The flag wins over VAULT_PROFILE, which wins over "ask if ambiguous".
	cmd.PersistentFlags().StringVarP(&profile, "profile", "p", os.Getenv("VAULT_PROFILE"),
		"Profile from the \"vault:\" list in "+configPath()+" [$VAULT_PROFILE].")
	cmd.AddCommand(newSecretsCommand(&profile))
	return cmd
}

func newSecretsCommand(profile *string) *cobra.Command {
	var (
		prefix     string
		nameFilter string
	)

	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Search a provider's secrets interactively and print the selection",
		Long: `Search secrets with fzf and print the selection as JSON.

--path is a prefix of the lines you see in fzf, resolved by the server rather
than filtered after the fact. Its segments name a Vault mount and path, or an
Infisical project, environment and folder:

  --path secret/team/db                # Vault
  --path orders-service/production/db  # Infisical

With no --path, everything the credentials can see is listed. See the README for
the full mapping.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			selected, err := resolveSecretProfile(*profile)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			store, err := newSecretStore(ctx, selected)
			if err != nil {
				return err
			}

			ui.Info("Listing secrets from %s (%s)",
				ui.Cyan.Render(selected.Profile), selected.Provider)
			secrets, err := store.list(ctx, prefix)
			if err != nil {
				return err
			}
			if nameFilter != "" {
				secrets = slices.DeleteFunc(secrets, func(ref secretRef) bool {
					return !strings.Contains(ref.Path, nameFilter)
				})
			}
			// Backends list concurrently, so sort for a stable fzf order.
			slices.SortFunc(secrets, func(a, b secretRef) int {
				return strings.Compare(a.Path, b.Path)
			})

			return pickAndPrint(secrets, secretRef.Display, "secret", "secret> ",
				func(ref secretRef) (string, any, error) {
					value, err := ref.get(ctx)
					return ref.Path, value, err
				})
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&prefix, "path", "", "Only list secrets under this prefix.")
	flags.StringVar(&nameFilter, "filter", "", "Keep only paths containing this substring.")
	return cmd
}

// splitPrefix cuts a --path prefix into its first n container segments plus
// whatever is left, so a backend can map them onto its own hierarchy. Missing
// segments come back empty, which every caller reads as "all of them".
func splitPrefix(prefix string, n int) (containers []string, rest string) {
	containers = make([]string, n)
	remaining := strings.Trim(prefix, "/")
	for i := range n {
		if remaining == "" {
			return containers, ""
		}
		containers[i], remaining, _ = strings.Cut(remaining, "/")
	}
	return containers, remaining
}

// httpError carries a status code so callers can tell "you may not see this"
// from a real failure. Both providers answer 403 for a path the credentials do
// not cover, which is skipped rather than fatal.
type httpError struct {
	status int
	body   string
}

func (e httpError) Error() string { return fmt.Sprintf("error %d: %s", e.status, e.body) }

// denied reports whether an API error means "not visible to these credentials"
// rather than a failure worth aborting for. 404 counts: Vault answers it for an
// empty path, and neither provider distinguishes empty from forbidden.
func denied(err error) bool {
	var apiErr httpError
	return errors.As(err, &apiErr) &&
		(apiErr.status == http.StatusForbidden || apiErr.status == http.StatusNotFound)
}

// httpClient is the one client every REST call in cu goes through: the secret
// backends and pwpush. DefaultTransport allows only 2 idle connections per
// host, so a listConcurrency-wide fan-out would drop most of its connections
// and pay a fresh TLS handshake on the next level of the walk.
var httpClient = &http.Client{Transport: idleTransport()}

func idleTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConnsPerHost = listConcurrency
	return t
}

// mtlsClient builds a dedicated http.Client when a profile's credentials
// configure mTLS, since a client cert is per-profile and can't be shared
// through the package-wide httpClient. Returns nil when none are set, so the
// caller falls back to httpClient.
func mtlsClient(p secretProvider) (*http.Client, error) {
	creds := p.Credentials
	if creds.ClientCert == "" && creds.CACert == "" {
		return nil, nil
	}

	tlsConfig := &tls.Config{}
	if creds.ClientCert != "" {
		if creds.ClientKey == "" {
			return nil, fmt.Errorf("profile %q sets client_cert without client_key", p.Profile)
		}
		cert, err := tls.LoadX509KeyPair(creds.ClientCert, creds.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("loading client_cert/client_key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	if creds.CACert != "" {
		pem, err := os.ReadFile(creds.CACert)
		if err != nil {
			return nil, fmt.Errorf("reading ca_cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_cert %q has no usable certificates", creds.CACert)
		}
		tlsConfig.RootCAs = pool
	}

	transport := idleTransport()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport}, nil
}

// apiRequest performs a JSON API call and returns the response body. Both
// backends talk plain REST, so they share one round-tripper; headers is what
// distinguishes them. client is nil for the common case and falls back to
// httpClient; a profile configuring mTLS passes its own.
func apiRequest(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body []byte) ([]byte, error) {
	if client == nil {
		client = httpClient
	}
	// The client has no timeout of its own, so a black-holed endpoint would
	// hang the command forever. Per call, since a walk makes many.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, httpError{status: resp.StatusCode, body: strings.TrimSpace(string(content))}
	}
	return content, nil
}
