# ☁️ CloudUtil

CLI `cu` is a wrapper for most common AWS and Azure cloud operations with interactive selection and beautiful output.

[![Python 3.12+](https://img.shields.io/badge/python-3.12+-blue.svg)](https://www.python.org/downloads/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![AWS](https://img.shields.io/badge/AWS-Cloud-orange.svg)](https://aws.amazon.com/)
[![Azure](https://img.shields.io/badge/Azure-Cloud-blue.svg)](https://azure.microsoft.com/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Cloud-blue.svg)](https://kubernetes.io/)

```bash
pip install -U git+https://github.com/Rishang/cloudutil.git
```

OR Build from source:

```bash
cd /tmp && git clone https://github.com/Rishang/cloudutil.git && cd cloudutil && uv build && pip install ./dist/cloudutil-*.tar.gz
```

## 📚 Table of Contents

- [☁️ CloudUtil](#️-cloudutil)
  - [📚 Table of Contents](#-table-of-contents)
  - [✨ Features](#-features)
  - [📦 Installation](#-installation)
    - [Requirements](#requirements)
  - [🚀 Usage](#-usage)
    - [Top-level commands](#top-level-commands)
    - [AWS Operations](#aws-operations)
      - [Console Login](#console-login)
      - [SSM Parameter Management](#ssm-parameter-management)
      - [SSM Instance Connections](#ssm-instance-connections)
      - [Secrets Manager](#secrets-manager)
      - [Decode Authorization Message](#decode-authorization-message)
      - [Advanced AWS Usage](#advanced-aws-usage)
        - [Custom Policy for Console Login](#custom-policy-for-console-login)
        - [Environment Variables](#environment-variables)
    - [Azure Operations (`az`)](#azure-operations-az)
      - [Key Vault Secrets](#key-vault-secrets)
    - [SQL Operations](#sql-operations)
    - [Kubernetes Operations](#kubernetes-operations)
      - [Kubernetes Secrets](#kubernetes-secrets)
      - [Kubernetes ConfigMaps](#kubernetes-configmaps)
      - [Kubernetes Cluster Context Switching](#kubernetes-cluster-context-switching)
    - [OS Utils](#os-utils)
      - [Shell History](#shell-history)
    - [Semantic Diff](#semantic-diff)
    - [Taskfile Operations](#taskfile-operations)
    - [Password Pusher Operations](#password-pusher-operations)
  - [🎯 Interactive Selection](#-interactive-selection)
  - [📋 Command Reference](#-command-reference)
  - [🔧 Development](#-development)
    - [Local Development](#local-development)

## ✨ Features

- 🚀 **Interactive AWS Console Login** - Generate federated console URLs with a scoped IAM policy file
- 🔐 **SSM Parameter Management** - Search and retrieve parameters with fuzzy finding
- 📡 **SSM Instance Connections** - Direct SSH and port forwarding through Systems Manager
- 🔑 **Secrets Manager Integration** - Interactive secret browsing with JSON formatting
- 🎯 **Fuzzy Selection** - Powered by `fzf` for lightning-fast interactive selection
- 🎨 **Beautiful Output** - Rich terminal interface with colors and formatting
- ⚡ **Profile & Region Support** - Seamless switching between AWS profiles and regions (where supported per command)
- 🐍 **SQL Database Management** - Declarative PostgreSQL configuration via YAML (`validate`, `execute`, `init`)
- 🎛️ **Kubernetes Operations** - Interactive Kubernetes secrets and ConfigMaps browsing via `kubectl`
- 🧰 **OS Utils** - Shell history search
- 🔍 **Semantic Diff** - Structural diff of JSON, YAML, and TOML config files with git-diff-style output
- 🗂️ **Taskfile Passthrough** - Run local Taskfile tasks via `cu task ...` with interactive terminal support
- 🔐 **Password Pusher Integration** - Configure Password Pusher, share secrets, and generate strong passwords

## 📦 Installation

```bash
pip install -U git+https://github.com/Rishang/cloudutil.git
```

OR

```bash
git clone https://github.com/Rishang/cloudutil.git && cd cloudutil && uv build && pip install ./dist/cloudutil-*.tar.gz
```

### Requirements

- Python 3.12+
- `fzf` for interactive selection
- [Only for AWS operations] AWS CLI configured with credentials
- [Only for Azure operations] Azure CLI (`az login` must be run primarily)
- [Only for Kubernetes operations] `kubectl` configured with access to your target cluster
- [Only for Taskfile operations] [Taskfile](https://taskfile.dev/) installed and configured
- [Only for Password Pusher operations] [Password Pusher](https://pwpush.com/) configured


```bash
# Install fzf (if not already installed)
# macOS
brew install fzf

# Ubuntu/Debian
sudo apt install fzf

# Or follow: https://github.com/junegunn/fzf#installation
```

## 🚀 Usage

### Top-level commands

The main entrypoint is `cu` (see `[project.scripts]` in `pyproject.toml`). Subcommands are wired in `cloudutil/cli.py`:

| Command | Module | Purpose |
|--------|--------|---------|
| `cu aws` | `cloudutil.aws.cli` | AWS (login, SSM, Secrets Manager, decode message) |
| `cu az` | `cloudutil.azure.cli` | Azure Key Vault secrets |
| `cu sql` | `cloudutil.sql.cli` | PostgreSQL config validate / execute / init |
| `cu os` | `cloudutil.os_utils.cli` | YAML diff, shell history |
| `cu k` | `cloudutil.k8s.cli` | Kubernetes secrets, ConfigMaps, context switch |
| `cu diff` | `cloudutil.diff.cli` | Semantic diff of JSON, YAML, and TOML config files |
| `cu pwpush` | `cloudutil.pwpush.cli` | Password Pusher |
| `cu task` | `cloudutil.task.cli` | Passthrough to the `task` binary |

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
[*] Opening URL in your default web browser...
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

**Example workflow:**
```
[*] Listing SSM parameters with prefix: /app/production/
[*] Found 24 parameters. Opening fzf for selection...
[*] Retrieving 3 selected parameters...
[+] Parameters retrieved successfully.

{
  "name": "/app/production/database/host",
  "value": "prod-db.example.com"
}
{
  "name": "/app/production/api/key",
  "value": "secret-api-key-value"
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

**Example output:**
```
[*] Found 12 secrets. Opening fzf for selection...
[*] Retrieving 2 selected secrets...
[+] Secrets retrieved successfully.


Name: 'prod/database/credentials'
Description: 'Production database credentials'
ARN: 'arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/database/credentials-AbCdEf'
Value (JSON):
{
  "username": "admin",
  "password": "super-secure-password",
  "host": "prod-db.example.com",
  "port": 5432
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

**Example output:**
```
{
  "allowed_actions": [],
  "denied_actions": [
    {
      "actions": ["s3:GetObject"],
      "api_call": "GetObject",
      "main_type": "s3",
      "condition": {},
      "resource": "arn:aws:s3:::my-bucket/*",
      "type": "s3",
      "condition_text": "",
      "effective_action": "s3:GetObject",
      "reason": "Access Denied",
      "encoded_error_message": "Access Denied"
    }
  ],
  "encoded_message": "AQAA...",
  "decoded_error": "Access Denied",
  "decoded_message": "The provided authorization message is invalid or has expired."
}
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
Content Type: 'password'
ID: 'https://my-key-vault.vault.azure.net/secrets/prod-db-password/...'
Value:
super-secret-value
```

### SQL Operations

PostgreSQL-oriented workflows (see `cloudutil/sql/cli.py`):

```bash
# Generate a sample YAML template (default: config.yaml)
cu sql init
cu sql init -o my-config.yaml

# Validate configuration without connecting
cu sql validate my-config.yaml

# Apply configuration
cu sql execute --config-file my-config.yaml
# short form:
cu sql execute -c my-config.yaml
```

### Kubernetes Operations

Browse Kubernetes resources interactively using `fzf`. Selected resources are printed as JSON in the terminal.

For **Secrets** and **ConfigMaps**, `fzf` lists **one line per data key**: `namespace/name/key`. Choosing a line prints **only that key’s value** (secrets are base64-decoded).

#### Kubernetes Secrets

View and inspect Secret keys. Each key appears as `namespace/secret-name/key`; values are base64-decoded when shown.

```bash
# Scan secrets across all namespaces
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

**Example output** (one selected key):
```
{
  "default/app-secret": {
    "DATABASE_URL": "postgres://app:***@db:5432/app"
  }
}
```

#### Kubernetes ConfigMaps

View and inspect ConfigMap keys. Each key appears as `namespace/configmap-name/key`, with the same namespace flags as secrets.

```bash
# Scan ConfigMaps across all namespaces
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

#### Kubernetes Cluster Context Switching

Switch kube contexts interactively using `fzf`.

```bash
# Pick a context from kubeconfig and switch to it
cu k ctx
```

Notes:
- Context names are read from your current kubeconfig (`kubectl config view -o json`).
- The selected context is applied with `kubectl config use-context` (process is replaced via `exec`).

### OS Utils

Utilities for local/dev workflows and config validation tasks.

#### Shell History

Search shell history with fzf (supports zsh and bash).

```bash
cu os history
```

### Semantic Diff

Compare JSON, YAML, and TOML config files structurally — not line by line. Output groups changes by top-level key with a git-diff-style layout and a summary table at the end.

#### Compare two files inline

```bash
cu diff -f prod.yaml -f stage.yaml
```

**Example output:**
```
--- a/prod.yaml  (main)
+++ b/stage.yaml  (main)

@@ app @@  (2 changes)
~  port:     8080 → 9090
~  version:  '1.0.0' → '2.0.0'

@@ database @@  (3 changes)
~  host:       'db.prod.internal' → 'db.stage.internal'
~  pool_size:  10 → 3
+  ssl:        True

 +  added   -  removed   ~  changed
────────────────────────────────────
        1            0            7
```

Branch name is shown automatically when the files are inside a git repository.

#### Compare using a config file

For multiple pairs or reusable ignore rules, use a config file:

```bash
cu diff --config diff_config.yaml
```

**Config format (`diff_config.yaml`):**

```yaml
global_ignore_keys:
  - metadata
  - status

global_ignore_patterns:
  - dev
  - TEST

diffs:
  - files:
      - config/prod.yaml
      - config/stage.yaml
    ignore_keys:
      - data

  - files:
      - service-a.toml
      - service-b.toml
```

#### Ignore rules

| Flag | Config field | Behaviour |
|------|-------------|-----------|
| `--ignore-key <key>` | `ignore_keys` / `global_ignore_keys` | Suppress any diff entry whose path contains this key segment (exact match, any depth) |
| `--ignore-pattern <str>` | `ignore_patterns` / `global_ignore_patterns` | Suppress any diff entry where old or new value contains this substring (case-insensitive) |

```bash
# Inline flags
cu diff -f prod.yaml -f stage.yaml \
  --ignore-key metadata \
  --ignore-key status \
  --ignore-pattern dev
```

#### Output formats

```bash
# Default: git-diff-style unified output
cu diff -f a.yaml -f b.yaml

# Rich table: symbol | path | old | new
cu diff -f a.yaml -f b.yaml --format table

# Machine-readable JSON (stdout)
cu diff -f a.yaml -f b.yaml --format json
```

Exit code is `0` when no differences are found, `1` otherwise — suitable for use in CI pipelines.

#### JMESPath YAML diff (formerly `cu os ydiff`)

Compare YAML nodes at a specific JMESPath location across multiple files. Useful for validating that config values match across environments.

```bash
cu diff --ydiff ydiff_config.yaml
```

**Config format (`ydiff_config.yaml`):**

```yaml
ydiff:
  - jsmec: "configMap"
    files:
      - app-v1: "./values/main.yaml"
      # $branch resolves to the current git branch name for that file path
      - $branch: "./values/feature.yaml"
    ignore_patterns:
      - test
      - dev
```

Notes:
- `jsmec` is the JMESPath expression to extract the node to compare.
- Every item under `files` is a single-key mapping: `{alias: path}`.
- At least 2 files are required per check.
- `$branch` auto-resolves to the current git branch name for that file path.

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

Manage temporary secret sharing with [Password Pusher](https://pwpush.com/) (`cloudutil/pwpush/cli.py`).

```bash
# Save Password Pusher config (requires --token, --source, and --email)
cu pwpush config --source https://pwpush.com --token <api-token> --email you@example.com

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
- Config is stored at `~/.config/cu/psst.json`.
- `send` uses bearer auth when no email is stored in config; legacy header auth when `email` is present in the saved config.

## 🎯 Interactive Selection

All commands use `fzf` for interactive selection, providing:

- **Fuzzy matching** - Type partial names to filter
- **Multi-select** - Use `Tab` to select multiple items
- **Real-time filtering** - Instant results as you type
- **Keyboard shortcuts** - Standard fzf navigation

## 📋 Command Reference

| Group | Commands |
|-------|----------|
| `cu aws` | `login`, `ssm-parameters`, `ec2-ssm`, `secrets`, `decode-message` |
| `cu az` | `secrets` |
| `cu sql` | `execute`, `validate`, `init` |
| `cu os` | `history` |
| `cu k` | `secrets`, `configmaps`, `ctx` |
| `cu diff` | `-f <a> -f <b>`, `--config <config.yaml>`, `--ydiff <config.yaml>` |
| `cu pwpush` | `config`, `send`, `list-active`, `pwgen` |
| `cu task` | forwards to `task -t <taskfile> -d <dir> ...` |

Run `cu --help` and `cu <group> --help` for live usage.

## 🔧 Development

### Local Development

```bash
git clone https://github.com/Rishang/cloudutil.git
cd cloudutil

# Install dependencies (creates .venv when using uv)
uv sync

# Run the CLI (console script from pyproject)
uv run cu --help

# Or activate the venv and run cu directly
source .venv/bin/activate
cu --help
```

---

<div align="center">
  <p>Made with ❤️ for the Cloud community</p>
  <p>
    <a href="https://github.com/Rishang/cloudutil/issues">Report Bug</a>
    ·
    <a href="https://github.com/Rishang/cloudutil/issues">Request Feature</a>
  </p>
</div>
