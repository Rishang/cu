package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/Rishang/cloudutil/internal/ui"
)

// vaultClient is the little slice of the Vault HTTP API cu needs: KV v2 list
// and read, plus userpass login when the profile has no token.
type vaultClient struct {
	endpoint  string
	token     string
	namespace string
}

func newVaultClient(ctx context.Context, p secretProvider) (secretStore, error) {
	client := &vaultClient{
		endpoint:  strings.TrimRight(p.Endpoint, "/"),
		token:     p.Credentials.Token,
		namespace: p.Credentials.Namespace,
	}
	if client.token != "" {
		return client, nil
	}

	if p.Credentials.Username == "" || p.Credentials.Password == "" {
		return nil, fmt.Errorf("profile %q needs either a token or a username and password", p.Profile)
	}
	body, _ := json.Marshal(map[string]string{"password": p.Credentials.Password})
	resp, err := client.request(ctx, http.MethodPost,
		"/v1/auth/userpass/login/"+url.PathEscape(p.Credentials.Username), body)
	if err != nil {
		return nil, fmt.Errorf("vault login failed: %w", err)
	}
	var login struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(resp, &login); err != nil {
		return nil, err
	}
	if login.Auth.ClientToken == "" {
		return nil, fmt.Errorf("vault login returned no client token")
	}
	client.token = login.Auth.ClientToken
	return client, nil
}

// vaultFolder is a place to run one LIST against.
type vaultFolder struct {
	mount string
	path  string
}

// list walks every mount the prefix covers. Its first segment names the mount
// and the rest is a path inside it, so --path secret/team/db lists that subtree
// of the secret mount and a bare --path secret lists all of it.
//
// A mount whose own path contains a slash cannot be addressed that way; pass
// its first segment and let --filter do the rest.
func (c *vaultClient) list(ctx context.Context, prefix string) ([]secretRef, error) {
	containers, base := splitPrefix(prefix, 1)

	mounts := []string{containers[0]}
	if containers[0] == "" {
		var err error
		if mounts, err = c.kvMounts(ctx); err != nil {
			return nil, err
		}
		ui.Info("Searching KV v2 mount(s): %s", ui.Cyan.Render(strings.Join(mounts, ", ")))
	}
	return c.walk(ctx, mounts, base)
}

// kvMounts lists the KV v2 mounts this token can see. sys/internal/ui/mounts is
// what the Vault UI itself calls: any authenticated token may read it and gets
// back only the mounts its policies allow, whereas sys/mounts needs a
// root-ish policy and 403s for everyone else.
func (c *vaultClient) kvMounts(ctx context.Context) ([]string, error) {
	resp, err := c.request(ctx, http.MethodGet, "/v1/sys/internal/ui/mounts", nil)
	if err != nil {
		return nil, fmt.Errorf("could not list secret engines (name one with --path to skip discovery): %w", err)
	}
	var listed struct {
		Data struct {
			Secret map[string]struct {
				Type    string            `json:"type"`
				Options map[string]string `json:"options"`
			} `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &listed); err != nil {
		return nil, err
	}

	var mounts, skipped []string
	for mountPath, mount := range listed.Data.Secret {
		if mount.Type != "kv" {
			continue
		}
		if mount.Options["version"] != "2" {
			skipped = append(skipped, strings.Trim(mountPath, "/"))
			continue
		}
		mounts = append(mounts, strings.Trim(mountPath, "/"))
	}
	if len(skipped) > 0 {
		sort.Strings(skipped)
		ui.Warn("Skipping KV v1 mount(s): %s", strings.Join(skipped, ", "))
	}
	if len(mounts) == 0 {
		return nil, fmt.Errorf("no KV v2 mounts visible to this token")
	}
	sort.Strings(mounts)
	return mounts, nil
}

// walk returns every secret under the given folders.
//
// It goes breadth-first: each level's folders are listed concurrently, then the
// folders they turned up become the next level. Recursion with a bounded
// errgroup would be shorter, but a parent blocked in Go() while holding a slot
// can deadlock against its own children; a barrier per level cannot. KV trees
// are wide and shallow, so the width is where the time was anyway.
func (c *vaultClient) walk(ctx context.Context, mounts []string, base string) ([]secretRef, error) {
	frontier := make([]vaultFolder, 0, len(mounts))
	for _, mount := range mounts {
		frontier = append(frontier, vaultFolder{mount: mount, path: base})
	}

	var found []secretRef
	for len(frontier) > 0 {
		// Indexed rather than appended to, so the result order is the tree's
		// and not whichever request finished first.
		secrets := make([][]secretRef, len(frontier))
		folders := make([][]vaultFolder, len(frontier))

		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(listConcurrency)
		for i, folder := range frontier {
			group.Go(func() error {
				var err error
				secrets[i], folders[i], err = c.listFolder(groupCtx, folder)
				return err
			})
		}
		if err := group.Wait(); err != nil {
			return nil, err
		}

		frontier = frontier[:0]
		for i := range secrets {
			found = append(found, secrets[i]...)
			frontier = append(frontier, folders[i]...)
		}
	}
	return found, nil
}

// listFolder is one LIST call, splitting the keys it returns into secrets and
// the folders still to descend into. Folders the token cannot list come back
// empty rather than failing the walk, since a policy that grants a subtree
// usually denies the level above it.
func (c *vaultClient) listFolder(ctx context.Context, folder vaultFolder) ([]secretRef, []vaultFolder, error) {
	resp, err := c.request(ctx, http.MethodGet,
		c.kvPath(folder.mount, "metadata", folder.path)+"?list=true", nil)
	if err != nil {
		// Vault answers 404 for an empty path and 403 for one this token may
		// not list; neither is worth aborting the whole walk for.
		if denied(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var listed struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &listed); err != nil {
		return nil, nil, err
	}

	var secrets []secretRef
	var folders []vaultFolder
	for _, key := range listed.Data.Keys {
		// Join drops the trailing slash that marks a folder, and the leading
		// one a root-level path would otherwise start with.
		child := vaultFolder{mount: folder.mount, path: path.Join(folder.path, key)}
		if strings.HasSuffix(key, "/") {
			folders = append(folders, child)
			continue
		}
		secrets = append(secrets, secretRef{
			Path: child.mount + "/" + child.path,
			get: func(ctx context.Context) (any, error) {
				return c.read(ctx, child)
			},
		})
	}
	return secrets, folders, nil
}

// read returns the current version's key/value data at a path.
func (c *vaultClient) read(ctx context.Context, secret vaultFolder) (map[string]any, error) {
	resp, err := c.request(ctx, http.MethodGet, c.kvPath(secret.mount, "data", secret.path), nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil, err
	}
	// Same as AWS and Azure: a value that is itself a JSON object is nested in
	// the output instead of escaped into a string, so jq can walk into it.
	for key, value := range parsed.Data.Data {
		if str, isString := value.(string); isString {
			parsed.Data.Data[key] = decodeSecretValue(str)
		}
	}
	return parsed.Data.Data, nil
}

// kvPath builds a KV v2 API path, where the mount is followed by the API verb
// and then the secret path: secret/metadata/team/db.
func (c *vaultClient) kvPath(mount, verb, secretPath string) string {
	// Join cleans empty and dotted segments; EscapedPath does the percent
	// encoding, so neither has to be hand-rolled per segment.
	joined := url.URL{Path: path.Join("/v1", mount, verb, secretPath)}
	return joined.EscapedPath()
}

func (c *vaultClient) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	return apiRequest(ctx, method, c.endpoint+path, map[string]string{
		"X-Vault-Token":     c.token,
		"X-Vault-Namespace": c.namespace,
	}, body)
}
