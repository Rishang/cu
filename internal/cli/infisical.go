package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/Rishang/cloudutil/internal/ui"
)

// infisicalClient is the slice of the Infisical API cu needs: list the projects
// an identity can see, then list secrets in a project's environment.
//
// Infisical's public API authenticates machine identities only — email and
// password are for the web dashboard and its own CLI, which do an SRP exchange
// plus MFA. So a profile carries either a static identity access token (Token
// Auth) or a Universal Auth client id and secret, which are exchanged here for
// a short-lived one.
type infisicalClient struct {
	endpoint string
	token    string
}

func newInfisicalClient(ctx context.Context, p secretProvider) (secretStore, error) {
	client := &infisicalClient{
		endpoint: strings.TrimRight(p.Endpoint, "/"),
		token:    p.Credentials.Token,
	}
	if client.token != "" {
		return client, nil
	}

	if p.Credentials.ClientID == "" || p.Credentials.ClientSecret == "" {
		return nil, fmt.Errorf(
			"profile %q needs either a token or a client_id and client_secret", p.Profile)
	}
	// organizationSlug matters only for sub-organization setups, but Infisical
	// rejects an empty one, so it is sent only when configured.
	login := map[string]string{
		"clientId":     p.Credentials.ClientID,
		"clientSecret": p.Credentials.ClientSecret,
	}
	if p.Credentials.Namespace != "" {
		login["organizationSlug"] = p.Credentials.Namespace
	}
	body, _ := json.Marshal(login)
	resp, err := client.request(ctx, http.MethodPost, "/api/v1/auth/universal-auth/login", body)
	if err != nil {
		return nil, fmt.Errorf("infisical login failed: %w", err)
	}
	var issued struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(resp, &issued); err != nil {
		return nil, err
	}
	if issued.AccessToken == "" {
		return nil, fmt.Errorf("infisical login returned no access token")
	}
	client.token = issued.AccessToken
	return client, nil
}

// infisicalScope is one project-and-environment pair, the unit Infisical lists
// secrets in. Its project id addresses the API; its slug names the fzf line.
type infisicalScope struct {
	projectID   string
	projectSlug string
	environment string
}

// list covers every project and environment the prefix allows. Infisical nests
// project → environment → folder, so the prefix's first two segments pick the
// project and environment and the rest is a folder path handed to the API as
// secretPath.
//
// Unlike Vault there is no tree to walk: one call per scope with recursive=true
// returns every secret beneath the path, so the fan-out is across scopes.
func (c *infisicalClient) list(ctx context.Context, prefix string) ([]secretRef, error) {
	containers, folder := splitPrefix(prefix, 2)
	project, environment := containers[0], containers[1]

	scopes, err := c.scopes(ctx, project, environment)
	if err != nil {
		return nil, err
	}
	if project == "" || environment == "" {
		ui.Info("Searching %d project/environment pair(s)", len(scopes))
	}

	found := make([][]secretRef, len(scopes))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(listConcurrency)
	for i, scope := range scopes {
		group.Go(func() error {
			var err error
			found[i], err = c.listScope(groupCtx, scope, folder)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	var secrets []secretRef
	for _, scoped := range found {
		secrets = append(secrets, scoped...)
	}
	return secrets, nil
}

// scopes resolves the project and environment names from a --path prefix into
// the pairs to list. Either may be empty, meaning all of them. A project is
// matched on its slug, name, or id, since which of those a user reaches for
// depends on where they copied it from.
func (c *infisicalClient) scopes(ctx context.Context, project, environment string) ([]infisicalScope, error) {
	resp, err := c.request(ctx, http.MethodGet, "/api/v1/projects", nil)
	if err != nil {
		return nil, fmt.Errorf("could not list projects: %w", err)
	}
	var listed struct {
		Projects []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Slug         string `json:"slug"`
			Environments []struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"environments"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(resp, &listed); err != nil {
		return nil, err
	}

	var scopes []infisicalScope
	for _, candidate := range listed.Projects {
		if project != "" &&
			project != candidate.Slug && project != candidate.Name && project != candidate.ID {
			continue
		}
		for _, env := range candidate.Environments {
			if environment != "" && environment != env.Slug && environment != env.Name {
				continue
			}
			scopes = append(scopes, infisicalScope{
				projectID:   candidate.ID,
				projectSlug: candidate.Slug,
				environment: env.Slug,
			})
		}
	}
	if len(scopes) == 0 {
		if project == "" {
			return nil, fmt.Errorf("no projects visible to these credentials")
		}
		return nil, fmt.Errorf("no project and environment matching %q", strings.Trim(
			project+"/"+environment, "/"))
	}
	return scopes, nil
}

// listScope lists one project environment. recursive=true covers every folder
// under the path in a single call. A scope the identity cannot read is skipped,
// since an identity is often added to only some of an organization's projects.
func (c *infisicalClient) listScope(ctx context.Context, scope infisicalScope, folder string) ([]secretRef, error) {
	query := url.Values{
		"projectId":   {scope.projectID},
		"environment": {scope.environment},
		"secretPath":  {"/" + folder},
		"recursive":   {"true"},
		// The list call returns the values, so nothing has to be read back.
		"viewSecretValue": {"true"},
	}
	resp, err := c.request(ctx, http.MethodGet, "/api/v4/secrets?"+query.Encode(), nil)
	if err != nil {
		if denied(err) {
			return nil, nil
		}
		return nil, err
	}
	var listed struct {
		Secrets []struct {
			SecretKey   string `json:"secretKey"`
			SecretValue string `json:"secretValue"`
			SecretPath  string `json:"secretPath"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(resp, &listed); err != nil {
		return nil, err
	}

	secrets := make([]secretRef, 0, len(listed.Secrets))
	for _, secret := range listed.Secrets {
		// A value that is itself a JSON object is nested rather than escaped,
		// the same as AWS, Azure and Vault secrets.
		value := decodeSecretValue(secret.SecretValue)
		secrets = append(secrets, secretRef{
			// secretPath is absolute within the environment ("/" or "/db"), and
			// Join drops the lone slash the root folder gives.
			Path: path.Join(scope.projectSlug, scope.environment, secret.SecretPath, secret.SecretKey),
			get:  func(context.Context) (any, error) { return value, nil },
		})
	}
	return secrets, nil
}

func (c *infisicalClient) request(ctx context.Context, method, endpointPath string, body []byte) ([]byte, error) {
	headers := map[string]string{"Accept": "application/json"}
	// No token yet while logging in, and an empty Bearer is worse than none.
	if c.token != "" {
		headers["Authorization"] = "Bearer " + c.token
	}
	return apiRequest(ctx, method, c.endpoint+endpointPath, headers, body)
}
