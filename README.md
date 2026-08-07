# ☁️ CloudUtil

`cu` browses and operates the things you already have credentials for — AWS, Azure
Key Vault, HashiCorp Vault, Infisical, Kubernetes — by fuzzy-picking from a list
and printing JSON. Plus a semantic config differ, format converters, Password
Pusher and a Taskfile passthrough.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

One static binary with `fzf` compiled in, so there is nothing else to install for
interactive selection. Status text goes to stderr and data to stdout, so every
command pipes into `jq`.

## 📚 Table of Contents

- [Installation](#-installation)
- [Usage](#-usage)
  - [AWS Operations](#aws-operations)
  - [Azure Operations (`az`)](#azure-operations-az)
  - [Secret Providers (`vault`)](#secret-providers-vault)
  - [Kubernetes Operations](#kubernetes-operations)
  - [OS Utils](#os-utils)
  - [Semantic Diff](#semantic-diff)
  - [Format Conversion](#format-conversion)
  - [Taskfile Operations](#taskfile-operations)
  - [Password Pusher Operations](#password-pusher-operations)
- [Command Reference](#-command-reference)
- [Development](#-development)

## 📦 Installation

```bash
# Install script (Linux and macOS) — picks a user-level bin directory
# already on your $PATH, and falls back to /usr/local/bin.
curl -fsSL https://raw.githubusercontent.com/Rishang/cloudutil/main/install.sh | bash

# With Homebrew
brew install Rishang/tap/cu

# With install-release
ir get https://github.com/Rishang/cloudutil

# With mise
mise use -g "github:Rishang/cloudutil[exe=cu]"
```

The install script defaults to the latest release, and takes `VERSION` and
`INSTALL_DIR` overrides:

```bash
curl -fsSL https://raw.githubusercontent.com/Rishang/cloudutil/main/install.sh \
  | VERSION=v1.0.0 INSTALL_DIR="$HOME/.local/bin" bash
```

Or grab a prebuilt binary for your platform from the
[releases page](https://github.com/Rishang/cloudutil/releases) — Linux, macOS and
Windows, amd64 and arm64.

Or build from source — see [Development](#-development).

### Requirements

Nothing at all for `cu diff`, `cu os history` and `cu pwpush` — the binary is
static and `fzf` is compiled in. The remaining commands drive tools you almost
certainly already have:

- [Only for AWS operations] AWS credentials configured (`~/.aws` or env vars — no AWS CLI install needed; `cu aws ec2-ssm` also needs the [Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html))
- [Only for Azure operations] Azure credentials configured (env vars, managed identity, or `az login` — no Azure CLI install needed unless you use `az login`)
- [Only for Kubernetes operations] `kubectl` configured with access to your target cluster — or a pod with a mounted ServiceAccount, see [Running inside a pod](#running-inside-a-pod)
- [Only for Taskfile operations] [Taskfile](https://taskfile.dev/) installed and configured
- [Only for Password Pusher operations] [Password Pusher](https://pwpush.com/) configured

### Shell Completion

Add one line to your shell's rc file:

```bash
# ~/.bashrc
eval "$(cu completion bash)"

# ~/.zshrc — after 'autoload -U compinit && compinit'
eval "$(cu completion zsh)"

# ~/.config/fish/config.fish
cu completion fish | source
```

Then restart your shell. `cu completion --help` prints the same list.

## 🚀 Usage

### Top-level commands

| Command | Purpose |
|--------|---------|
| `cu aws` | AWS: console login, SSM, Secrets Manager, decode message |
| `cu az` | Azure Key Vault secrets |
| `cu os` | Shell history search |
| `cu k` | Kubernetes secrets, ConfigMaps, pod logs, pod exec, context and namespace switch |
| `cu diff` | Semantic diff of JSON, YAML, TOML, and HCL config files |
| `cu vault` | HashiCorp Vault and Infisical secrets |
| `cu json2yaml`, `cu yaml2json` | Format conversion on stdin or a file |
| `cu pwpush` | Password Pusher |
| `cu task` | Passthrough to the `task` binary |

### AWS Operations

#### Console Login

Generates a temporary AWS console login URL using STS `GetFederationToken`. A **policy JSON file is required** (`-f` / `--policy-file`).

```bash
# Policy file is required — example: read-only S3 policy in ./read-only-policy.json
cu aws login -f ./read-only-policy.json

# With profile and session duration (hours, default 2, range 1–24)
cu aws login -f ./read-only-policy.json --profile my-profile --duration 4

# Just print URL (don't open browser)
cu aws login -f ./read-only-policy.json --no-open
```

**Example output:**
```
[*] Using policy from file: ./read-only-policy.json
[*] Using AWS (profile: default, region: us-east-1)
[*] Requesting federation token for 'you' (duration: 7200s)...
[+] Federation token received.
[+] Console login URL generated (session valid for 7200s).
[*] Opening URL in your default browser (xdg-open)...
[+] Done. Check your browser.
```

#### SSM Parameter Management

Interactively search and retrieve SSM parameters:

```bash
# Search parameters (default prefix /)
cu aws ssm-parameters

# Search with prefix
cu aws ssm-parameters --prefix /app/production/

# With specific profile and region
cu aws ssm-parameters --prefix /app/ --profile prod --region eu-west-1
```

Multi-select with `Tab`. The selection is printed to stdout as one JSON object
keyed by parameter name; the status lines go to stderr, so `| jq` sees only the
data.

**Example workflow:**
```
[*] Listing SSM parameters with prefix: /app/production/
[*] Found 24 SSM parameter(s). Opening fzf for selection...

{
  "/app/production/api/endpoint": "https://api.internal.example.com",
  "/app/production/api/key": "secret-api-key-value"
}
```

#### SSM Instance Connections

Connect to EC2 instances through Systems Manager:

```bash
# Interactive instance selection and direct connection
cu aws ec2-ssm

# Port forwarding tunnel
cu aws ec2-ssm --tunnel --remote-host localhost --remote-port 5432 --local-port 5432
```

**Example workflow:**
```
[*] Found 8 instances. Opening fzf for selection...
# Select: i-0123456789abcdef0 | web-server-prod
# Connects directly to the instance via SSM
```

#### Secrets Manager

Browse and retrieve secrets with automatic JSON parsing:

```bash
# Search all secrets
cu aws secrets

# Filter by name prefix
cu aws secrets --filter "prod/"

# With specific profile and region
cu aws secrets --filter "app/" --profile production --region us-east-1
```

A secret whose value parses as a JSON object is nested in the output; anything
else is passed through as a plain string.

**Example output:**
```
[*] Listing secrets with filter: prod/
[*] Found 12 AWS secret(s). Opening fzf for selection...

{
  "prod/service/credentials": {
    "username": "svc-app",
    "password": "super-secure-password",
    "endpoint": "https://api.internal.example.com"
  },
  "prod/api/webhook-url": "https://hooks.example.com/T00000/B00000"
}
```

#### Decode Authorization Message

Decode an AWS authorization failure message using IAM's `decode_authorization_message` API:

```bash
# Decode a message interactively (opens $EDITOR, default vim)
cu aws decode-message

# Decode a specific message
cu aws decode-message --message "AQAA...<encoded message>..."
```

STS's decoded payload is printed to stdout verbatim — `cu` does not reshape it,
so pipe it to `jq` if you want it formatted.

**Example output** (abridged):
```
{"allowed":false,"explicitDeny":true,"matchedStatements":{"items":[]},
 "failures":{"items":[]},
 "context":{"principal":{"id":"AROA...:session","arn":"arn:aws:sts::123456789012:assumed-role/dev/session"},
            "action":"s3:GetObject","resource":"arn:aws:s3:::my-bucket/*"}}
```

#### Advanced AWS Usage

##### Custom Policy for Console Login

Create a JSON policy file to restrict console permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": "*"
    }
  ]
}
```

```bash
cu aws login -f ./s3-read-only.json
```

##### Environment Variables

CloudUtil respects standard AWS environment variables:

```bash
export AWS_PROFILE=my-profile
export AWS_DEFAULT_REGION=us-west-2
cu aws ssm-parameters  # Uses the environment settings
```

### Azure Operations (`az`)

Azure commands are under **`cu az`** (not `cu azure`).

#### Key Vault Secrets

Browse and retrieve Azure Key Vault secrets with automatic JSON parsing:

```bash
# Search all secrets in a vault
cu az secrets --vault my-key-vault

# Filter by name prefix
cu az secrets --vault my-key-vault --filter "prod-"

# JSON output (quieter logging)
cu az secrets --vault my-key-vault -o json
```

**Example output:**
```
[*] Listing secrets from vault my-key-vault with filter: prod-
[*] Found 5 secrets. Opening fzf for selection...
[*] Retrieving 1 selected secrets...
[+] Secrets retrieved successfully.

Name: 'prod-db-password'
Description: 'password'
ID: 'https://my-key-vault.vault.azure.net/secrets/prod-db-password/...'
Value: super-secret-value
────────────────────────────────────────────────────────────────────────
```

`Description` carries the secret's Key Vault content type. As with AWS, a value
that parses as a JSON object is printed as nested JSON instead of a raw string.

### Secret Providers (`vault`)

Unlike AWS and Azure, HashiCorp Vault and Infisical have no ambient credential
chain, so connections come from the `vault:` list in `~/.config/cu/config.yml`.
Each entry names its `provider`, so one command browses both backends:

```yaml
vault:
  - profile: prod
    provider: hashicorp        # "vault" still works, it was the old name
    endpoint: https://vault.example.com
    credentials:
      # Either a token, or a username and password for the userpass auth method.
      token: hvs.CAESxxxx
      username: ""
      password: ""
      namespace: default        # Vault Enterprise namespace; omit on OSS

  - profile: inf-prod
    provider: infisical
    endpoint: https://us.infisical.com   # or eu.infisical.com, or self-hosted
    credentials:
      # Either a machine identity access token (Token Auth), used as-is...
      token: st.xxxxx
      # ...or a Universal Auth pair, exchanged for a short-lived token at startup.
      client_id: 8f1a...
      client_secret: 4c2b...
      namespace: my-org         # organizationSlug; only for sub-organizations
```

Keep it owner-readable only: `chmod 600 ~/.config/cu/config.yml`.

Any value in `config.yml` (`psst` included) can reference an environment
variable instead of holding the secret in the file: `token: ${VAULT_TOKEN}` is
replaced with `$VAULT_TOKEN`'s value when the file is loaded. An unset
variable expands to empty; bare `$VAR` (no braces) is left untouched.

```yaml
psst:
  token: ${PSST_TOKEN}
  source: https://pwpush.example.com

vault:
  - profile: prod
    provider: hashicorp
    endpoint: https://vault.example.com
    credentials:
      token: ${VAULT_PROD_TOKEN}
```

```bash
export PSST_TOKEN=... VAULT_PROD_TOKEN=...
cu pwpush list-active   # config.yml never holds the raw tokens on disk
```

Infisical's public API authenticates **machine identities only** — email and
password are for the web dashboard and its own CLI, which use an SRP exchange
plus MFA. Create an identity, add it to the projects it should reach, and use
either of the two credential shapes above.

```bash
# Pick the profile interactively when the file has more than one
cu vault secrets

# -p is persistent, so it works before or after the subcommand
cu vault -p prod secrets
cu vault secrets --profile inf-prod
export VAULT_PROFILE=prod    # default for -p
```

#### Configuring from the environment

Every field of a profile has an environment variable, named for the provider it
belongs to. They win over the file, so CI can override one credential without
rewriting the config:

```bash
export VAULT_PROVIDER=hashicorp        # hashicorp | infisical — selects the block below

# Used when VAULT_PROVIDER=hashicorp
export VAULT_HASHICORP_ENDPOINT=https://vault.example.com
export VAULT_HASHICORP_TOKEN=hvs.CAESxxxx
export VAULT_HASHICORP_USERNAME=       # or userpass instead of a token
export VAULT_HASHICORP_PASSWORD=
export VAULT_HASHICORP_NAMESPACE=default

# Used when VAULT_PROVIDER=infisical
export VAULT_INFISICAL_ENDPOINT=https://us.infisical.com
export VAULT_INFISICAL_TOKEN=st.xxxxx
export VAULT_INFISICAL_CLIENT_ID=8f1a...
export VAULT_INFISICAL_CLIENT_SECRET=4c2b...
export VAULT_INFISICAL_NAMESPACE=my-org
```

Two traps worth knowing:

- `VAULT_PROVIDER` alone is a whole connection, so with it set and no `--profile`
  (or `VAULT_PROFILE`) `cu` never reads the `vault:` list — it will not
  prompt for a profile either. Name a profile and the environment goes back to
  overriding only the fields it sets; the profile's own `provider` decides which
  block is read, and a `VAULT_PROVIDER` that contradicts it is an error rather
  than a silent half-and-half connection.
- Only the selected provider's block is looked at. A leftover
  `VAULT_HASHICORP_TOKEN` cannot leak into an Infisical connection.

An empty export is the same as an unset one — it falls through to the file
rather than blanking the field.

#### Scoping with `--path`

`--path` is a prefix of the lines you see in fzf, so it narrows by whatever the
provider's outermost container is. Every segment is resolved by the server, not
filtered afterwards, so a narrow path also means fewer API calls:

| `--path` | Vault | Infisical |
|---|---|---|
| *(omitted)* | every KV v2 mount | every project × environment |
| `a` | mount `a` | project `a`, all environments |
| `a/b` | path `b` in mount `a` | project `a`, environment `b` |
| `a/b/c` | path `b/c` in mount `a` | folder `/c` in project `a`, environment `b` |

An Infisical project matches on its slug, name, or id. A Vault mount whose own
path contains a slash can only be addressed by its first segment — use `--filter`
for the rest.

`--filter` keeps only paths containing a substring, applied after listing.

#### What gets listed

Both providers list everything the credentials can see and skip what they cannot:
a Vault subtree your policy denies, or an Infisical project your identity was
never added to, is passed over rather than failing the run.

- **Vault** discovers mounts via `sys/internal/ui/mounts` — the endpoint the Vault
  UI itself uses, readable by any authenticated token, unlike `sys/mounts` which
  needs a root-ish policy. KV **v1** mounts are named once in a warning and
  skipped. The tree is walked breadth-first, one bounded batch of LIST calls per
  level, so a wide mount does not serialise.
- **Infisical** discovers projects and their environments via `/api/v1/projects`,
  then makes one `recursive=true` call per project/environment pair — there is no
  tree to walk. Requires an Infisical with the v4 secrets API.

Selections print as one JSON object keyed by the path, and a value that is itself
a JSON object is nested rather than escaped — same as `cu aws secrets`:

```console
$ cu vault -p prod secrets --path secret/team
[*] Listing secrets from prod (vault)
[*] Searching KV v2 mount(s): secret
[*] Found 12 secret(s). Opening fzf for selection...
{
  "secret/team/backend/db": {
    "password": "super-secret-value",
    "username": "app"
  }
}
```

```console
$ cu vault -p inf-prod secrets
[*] Listing secrets from inf-prod (infisical)
[*] Searching 6 project/environment pair(s)
[*] Found 84 secret(s). Opening fzf for selection...
{
  "orders-service/production/db/DB_PASSWORD": "super-secret-value"
}
```

A Vault line names a secret and prints all of its keys; an Infisical line names a
single key, since that is the unit Infisical stores.

On Infisical Cloud, listing with no `--path` costs one API call per
project/environment pair against a limit of 120 secret reads a minute on the Free
plan (300 on Pro). Concurrency is capped at 8; self-hosted instances are
unlimited.

### Kubernetes Operations

Browse Kubernetes resources interactively using `fzf`. Selected resources are printed as JSON in the terminal.

For **Secrets** and **ConfigMaps**, `fzf` lists **one line per data key**: `namespace/name/key`. Choosing a line prints **only that key’s value** (secrets are base64-decoded).

Namespace resolution is the same everywhere: `-A` searches every namespace, `-n
<ns>` searches that one, and with neither flag you get whatever your current
context points at — exactly what bare `kubectl get` would do.

#### Kubernetes Secrets

View and inspect Secret keys. Each key appears as `namespace/secret-name/key`; values are base64-decoded when shown.

```bash
# Scan the current context's namespace
cu k secrets

# Scan a specific namespace
cu k secrets --namespace default

# Explicitly scan all namespaces
cu k secrets --all-namespaces
# or
cu k secrets -A

# Choose namespace interactively first, then pick secrets
cu k secrets --select-namespace
```

**Example output** (two selected keys — the JSON is keyed by the full
`namespace/name/key` path, so multi-select stays unambiguous):
```
{
  "default/app-secret/API_TOKEN": "s3cr3t",
  "default/app-secret/WEBHOOK_URL": "https://hooks.example.com/T00000"
}
```

#### Kubernetes ConfigMaps

View and inspect ConfigMap keys. Each key appears as `namespace/configmap-name/key`, with the same namespace flags as secrets.

```bash
# Scan the current context's namespace
cu k configmaps

# Scan a specific namespace
cu k configmaps --namespace kube-system

# Explicitly scan all namespaces
cu k configmaps --all-namespaces
# or
cu k configmaps -A

# Choose namespace interactively first, then pick ConfigMaps
cu k configmaps --select-namespace
```

**Example output** (one selected key):
```
{
  "kube-system/coredns": {
    "Corefile": ".:53 {\n    errors\n    health\n    kubernetes cluster.local in-addr.arpa ip6.arpa\n    ...\n}"
  }
}
```

#### Pod Logs

Fuzzy-pick a pod and stream its logs — a `kubectl logs` front end, not a
reimplementation.

```bash
# Pick a pod in the current namespace and print its logs
cu k logs

# Follow a specific namespace, last 100 lines
cu k logs -n prod -f --tail 100

# Search every namespace, follow, redirect to a file
cu k logs -A -f > app.log
```

| Flag | Meaning |
|------|---------|
| `-n`, `--namespace` | Namespace to search (default: the current context's) |
| `-A`, `--all-namespaces` | Search every namespace |
| `-f`, `--follow` | Stream new lines as they arrive |
| `-t`, `--tail` | Recent lines to show (`-1`, the default, means the whole log) |

Logs go to stdout and the status lines to stderr, so `> file`, `| grep` and
`| jq` all work without a flag for it.

The picker carries a preview panel on the right showing the last 50 lines of
whichever pod is highlighted, so you can find the one actually misbehaving
before committing to it:

```
▌ kube-system/coredns-589f44dc88-qvpq4 (Running)  │ maxprocs: Leaving GOMAXPROCS=12: CPU quota undefined
▌ kube-system/coredns-589f44dc88-q9znc (Running)  │ .:53
  2/13 ───────────────────────────────────────────│ CoreDNS-1.14.2
pod> coredns                                      │ linux/amd64, go1.26.1, dd1df4f
```

Long lines wrap, with continuations indented rather than marked. The panel is
resizable: **drag its left border** with the mouse, or cycle 75% / 25% / 50%
with **ctrl-]** if you would rather not reach for it. **ctrl-o** hides it
entirely — worth knowing, since it costs one `kubectl logs` call per cursor
move.

Its depth is fixed at 50 lines and does not follow `--tail` — it is there to
tell pods apart, not to read the log.

Pods are listed as `namespace/name (phase)`, so typing `crash` finds the
CrashLoopBackOff one. Selection is single: one pod, one stream. A pod with
several containers gets kubectl's `--prefix`, so every line says which container
it came from:

```
[pod/api-7d9f-abc/app]    {"level":"info","msg":"listening on :8080"}
[pod/api-7d9f-abc/envoy]  [info] listener manager: all dependencies initialized
```

#### Pod Exec

Fuzzy-pick a pod and run something in it. Same picker and same preview panel as
`cu k logs`, so you can land in the pod that is actually misbehaving.

```bash
# Interactive shell in a pod in the current namespace
cu k exec

# A different shell, in a named namespace
cu k exec -n prod -- bash

# One command, non-interactively — exits with the command's own status
cu k exec -A -- cat /etc/hosts
```

| Flag | Meaning |
|------|---------|
| `-n`, `--namespace` | Namespace to search (default: the current context's) |
| `-A`, `--all-namespaces` | Search every namespace |
| `-c`, `--container` | Container to exec into; prompted for when the pod has several |

The default command is `sh`, not `bash` — it exists in alpine and other minimal
images where `bash` does not. Anything after `--` replaces it, keeping its own
flags.

The TTY is requested only when your stdin is one, so `echo data | cu k exec -- tee
/tmp/x` pipes cleanly instead of being mangled by terminal processing.

#### Kubernetes Context and Namespace Switching

`ctx` is a `kubectx` equivalent and `ns` a `kubens` one.

```bash
# Pick a context from kubeconfig and switch to it
cu k ctx

# Pick a namespace and make it the current context's default
cu k ns

# Skip fzf entirely — handy in scripts
cu k ctx prod-cluster
cu k ns staging
```

Notes:
- Contexts come from `kubectl config get-contexts -o name`, namespaces from `kubectl get namespaces -o name`.
- `ctx` applies the choice with `kubectl config use-context`; `ns` uses `kubectl config set-context --current --namespace`. kubectl's confirmation goes to stderr.
- Neither validates a name passed as an argument — kubectl accepts any string for `--namespace`, so a typo sets a namespace that does not exist.

The active context and namespace are highlighted in green in the fzf list, so
`cu k ctx` doubles as "which cluster am I on?". Under `NO_COLOR` or a dumb
terminal the active entry is suffixed with `(current)` instead.

#### Running inside a pod

`cu k` drives `kubectl`, and `kubectl` — unlike the client libraries — has **no
in-cluster fallback**: given no kubeconfig it fails rather than reading the
ServiceAccount token every pod already has mounted.

So `cu` supplies one. When all of the following hold, it writes a kubeconfig to
`$TMPDIR/cu-in-cluster.kubeconfig` and runs `kubectl` against it:

- `$KUBERNETES_SERVICE_HOST` is set (i.e. running in a pod)
- `$KUBECONFIG` is unset and `~/.kube/config` does not exist
- `/var/run/secrets/kubernetes.io/serviceaccount/token` exists

Any kubeconfig you provide yourself always wins — it was put there on purpose.

The generated config references the token **by path**, never by value, so no
credential is copied anywhere, nothing lands in `ps` output, and `kubectl`
re-reads the file as the kubelet rotates it. The pod's own namespace becomes the
default, since that is what its RBAC most likely covers.

What that means per command, with a namespace-scoped Role:

| Command | In-pod behaviour |
|---|---|
| `cu k secrets` / `configmaps` / `logs` | Work within the Role's namespace. Use `-n <ns>`; `-A` issues a cluster-wide list and fails with Forbidden |
| `cu k exec` | Also needs `create` on `pods/exec`, which `get`/`list` on pods does not imply |
| `cu k ns` | Needs cluster-wide `list namespaces` — fails unless the Role grants it |
| `cu k ctx` | Lists the single `in-cluster` context |

### OS Utils

#### Shell History

Search shell history with fzf (supports zsh and bash).

```bash
cu os history
```

### Semantic Diff

Compare JSON, YAML, TOML, and HCL/Terraform config files structurally — not line by line. Default output is a rich table; use `--unified` / `-u` for git-diff style.

Supported file types: `.json`, `.yaml`, `.yml`, `.toml`, `.tf`, `.hcl`, `.tfvars`

#### Auto-detection

Place a `cu_diff.yml` in your working directory and run `cu diff` with no flags — it is picked up automatically.

#### Compare two files inline

```bash
cu diff -f prod.yaml -f stage.yaml
```

**Example output (table — default):**
```
── DIFF ────────────────────────────────────────────────
  −  prod.yaml  (main)
  +  stage.yaml  (main)

╭───┬──────────────────┬─────────────────────┬─────────────────────╮
│   │  Path            │  − prod.yaml (main) │  + stage.yaml (main)│
├───┼──────────────────┼─────────────────────┼─────────────────────┤
│ ~ │  app.port        │  8080               │  9090               │
│ ~ │  app.version     │  '1.0.0'            │  '2.0.0'            │
│ + │  cache.enabled   │  —                  │  True               │
╰───┴──────────────────┴─────────────────────┴─────────────────────╯

  +  added   -  removed   ~  changed
──────────────────────────────────────
        1            0            2
```

Switch to git-diff style with `--unified` / `-u`:

```bash
cu diff -f prod.yaml -f stage.yaml --unified
```

#### N-way comparison

Pass 3 or more `-f` flags — all N-choose-2 pairs are compared automatically:

```bash
cu diff -f dev.yaml -f stage.yaml -f prod.yaml
# compares: dev↔stage, dev↔prod, stage↔prod
```

#### Filter with a path prefix or JMESPath (`-q`)

```bash
# Show only diffs under configmap.data
cu diff -f qa/values.yaml -f prod/values.yaml -q "configmap.data"

# JMESPath expression
cu diff -f a.yaml -f b.yaml -q "[?kind=='changed']"
```

#### Ignore rules

| Flag | Config field | Behaviour |
|------|-------------|-----------|
| `--ignore-key <seg>` | `ignore_keys` / `global_ignore_keys` | Suppress any path whose segments contain this key (exact match, any depth) |
| `--ignore-pattern <tok>` | `ignore_patterns` / `global_ignore_patterns` | Strip these tokens from both values, then compare — suppressed if equal |

`--ignore-pattern` uses word-boundary regex and accepts comma-separated values:

```bash
# Suppress env-name-only differences (qa-server vs prod-server → -server = -server)
cu diff -f qa/values.yaml -f prod/values.yaml \
  --ignore-key metadata \
  --ignore-pattern "qa,prod,stage"
```

Suppressed entries are shown in an **⊘ Ignored** block above the table so you can verify what was filtered.

#### Output formats

```bash
# Table (default)
cu diff -f a.yaml -f b.yaml

# Git-diff style
cu diff -f a.yaml -f b.yaml --unified
cu diff -f a.yaml -f b.yaml -u

# Machine-readable JSON
cu diff -f a.yaml -f b.yaml -o json
cu diff -f a.yaml -f b.yaml --format json
```

Exit code is `0` when no differences are found, `1` otherwise — suitable for CI pipelines.

#### Compare using a config file

For multiple pairs, global ignore rules, or reusable queries, use a `cu_diff.yml` config file:

```bash
cu diff --config cu_diff.yml
# or just: cu diff   (auto-detects cu_diff.yml in CWD)
```

**Config format (`cu_diff.yml`):**

```yaml
format: table                        # default output format (table | unified | json)
query: configmap.data                # global path prefix or JMESPath filter
global_ignore_keys:
  - metadata
  - status
global_ignore_patterns: "qa,prod,stage"   # comma-separated string or YAML list

diffs:
  # Two-way diff
  - files:
      - k8s-manifest/helm/admin/values.yaml
      - k8s-manifest-bak/helm/admin/values.yaml
    query: configmap.data            # per-pair override

  # N-way diff — all 3 pairs compared automatically
  - files:
      - helm/app/values-dev.yaml
      - helm/app/values-stage.yaml
      - helm/app/values-prod.yaml
    ignore_keys:
      - timestamp

  # Terraform / HCL
  - files:
      - infra/main.tf
      - infra-bak/main.tf
```

Config file paths are resolved relative to the config file's location, so you can run `cu diff` from any directory.

#### Print config schema

```bash
cu diff --print-schema    # schema for cu_diff.yml
```

The schema output is designed to be fed to an AI/CLI agent. Pipe it along with your file list to auto-generate a valid `cu_diff.yml`:

```bash
# Let an LLM generate cu_diff.yml for your files
cu diff --print-schema | llm "Generate a cu_diff.yml for these helm values files: \
  k8s/helm/*/values-qa.yaml vs k8s/helm/*/values-prod.yaml \
  with ignore_patterns: qa,prod"
```

### Format Conversion

```bash
cat file.json | cu json2yaml
cu yaml2json -f manifest.yaml
```

Both read stdin or `-f <file>` and write to stdout. `yaml2json` emits one JSON
object per line for a multi-document (`---`) file: JSON has no multi-document
form, and dropping every document but the first would be silent data loss.

### Taskfile Operations

Run [Taskfile](https://taskfile.dev/) tasks through CloudUtil. `cu task` replaces the current process with `task`, forwarding extra arguments for full interactive TTY behavior.

Default Taskfile: `~/.config/cu/Taskfile.yml`. Default directory: current working directory.

```bash
# Run default task
cu task default

# Run any task with additional flags/args (after --)
cu task deploy -- --env prod

# Custom Taskfile and working directory
cu task -t ./Taskfile.yml -d /path/to/project default
cu task --taskfile ./Taskfile.yml --directory . deploy

# Task passthrough help
cu task --help
```

### Password Pusher Operations

Manage temporary secret sharing with [Password Pusher](https://pwpush.com/).

```bash
# Save Password Pusher config (--token and --source required; --email only for
# self-hosted instances still on legacy header auth)
cu pwpush config --source https://pwpush.com --token <api-token>

# Send a secret (opens $EDITOR if --file is omitted)
cu pwpush send --note "prod db password" --days 7 --views 5

# Send secret from file
cu pwpush send --file ./secret.txt --note "vpn creds"

# List active pushes
cu pwpush list-active

# Generate a random password
cu pwpush pwgen --length 24
```

Notes:
- Config is stored under the `psst:` key of `~/.config/cu/config.yml`, mode `0600` — it holds an API token.
- `send` uses bearer auth when no email is stored in config; legacy header auth when `email` is present in the saved config.

## 🎯 Interactive Selection

`fzf` is the real thing, linked into `cu` rather than shelled out to, so there is
no separate install and no `fzf: command not found`. Every keybinding is the one
you already know, `Tab` multi-selects where a command accepts more than one item,
and `$FZF_DEFAULT_OPTS` / `$FZF_DEFAULT_OPTS_FILE` are honored as usual.

## 📋 Command Reference

| Group | Commands |
|-------|----------|
| `cu aws` | `login`, `ssm-parameters`, `ec2-ssm`, `secrets`, `decode-message` |
| `cu az` | `secrets` |
| `cu os` | `history` |
| `cu k` | `secrets`, `configmaps`, `logs`, `exec`, `ctx`, `ns` |
| `cu diff` | `-f <a> -f <b>`, `--config <config.yaml>` |
| `cu vault` | `secrets` |
| *(top level)* | `json2yaml`, `yaml2json`, `completion` |
| `cu pwpush` | `config`, `send`, `list-active`, `pwgen` |
| `cu task` | forwards to `task -t <taskfile> -d <dir> ...` |

Run `cu --help` and `cu <group> --help` for live usage.

## 🔧 Development

Build through the Taskfile — it stamps the version metadata the release build
does, which a bare `go build` leaves empty:

```bash
git clone https://github.com/Rishang/cloudutil.git && cd cloudutil

task build    # -> dist/cu
task test     # go test ./... with the race detector
task lint     # gofmt + go vet
task release  # local goreleaser snapshot into dist/
```

`task --list-all` shows the rest. [AGENTS.md](AGENTS.md) has the code layout and
conventions.

Bugs and feature requests: [issues](https://github.com/Rishang/cloudutil/issues).
