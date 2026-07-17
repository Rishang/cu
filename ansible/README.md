# Ansible: CloudUtil PostgreSQL role

## Table of contents

- [Intro](#intro)
- [How it works / architecture](#how-it-works--architecture)
- [Getting started](#getting-started)
- [Examples](#examples)
- [Schemas](#schemas)
- [Native module mapping](#native-module-mapping)
- [Testing & CI](#testing--ci)
- [TL;DR](#tldr)

## Intro

CloudUtil PostgreSQL provisioning uses the native `cloudutil_postgres` role at `ansible/roles/cloudutil_postgres`. A role-local Pydantic gateway (`schemas/cloudutil_sql.py`) validates the existing YAML schema, resolves `${ENV_VAR}`, and renders `custom_sql` before native `community.postgresql` tasks apply it. The role does **not** import or require the `cloudutil` Python package — it's fully self-contained.

## How it works / architecture

```
playbook
  -> filter_plugins/cloudutil_sql_filters.py
    -> schemas/cloudutil_sql.py (Pydantic SQLConfig)
      validate -> resolve ${ENV_VAR} -> render custom_sql Jinja
    -> tasks/main.yml (community.postgresql modules)
      database -> extensions -> users -> privileges -> custom_sql
```

Pass the schema by **path** (`cloudutil_postgres_config_file`), not `vars_files` — that would let Ansible template `custom_sql`'s `{{ ... }}` before the gateway renders it with its own loader/env.

## Getting started

```bash
cd ansible
uv sync --locked
uv run ansible-galaxy collection install -r requirements.yml -p collections --force
export ANSIBLE_ROLES_PATH="$PWD/roles${ANSIBLE_ROLES_PATH:+:$ANSIBLE_ROLES_PATH}"
export ANSIBLE_COLLECTIONS_PATH="$PWD/collections${ANSIBLE_COLLECTIONS_PATH:+:$ANSIBLE_COLLECTIONS_PATH}"
```

`pyproject.toml`/`uv.lock` pin controller deps; `requirements.yml` pins the Galaxy collection. The managed host needs `psycopg2` or `psycopg` (that's where `community.postgresql` runs) — use `hosts: localhost` + `connection: local` to make the controller the managed host.

`Taskfile.yml` wraps the common commands:

```bash
task build   # docker build -f Dockerfile -t ansible-sql-worker .
task test    # bash test/run.sh — mock-RDS end-to-end cases
task lint    # ruff format, ruff check --fix --unsafe-fixes, ty check
```

Or use the standalone image (no CloudUtil package baked in):

```bash
docker build -f ansible/Dockerfile -t cloudutil-postgres-ansible .
docker run --rm --network host \
  -v "$PWD:/work" -w /work \
  cloudutil-postgres-ansible -i localhost, playbook.yml
```

`--network host` only makes sense on Linux, connecting to a DB exposed on the Docker host; omit it for a remote RDS endpoint.

## Examples

1. Write a schema file describing the provider, databases, users, and any custom SQL. For example `postgres.yaml`:

   ```yaml
   provider:
     name: postgres
     version: "16"
     host: ${POSTGRES_HOST}
     port: 5432
     username: ${POSTGRES_USER}
     password: ${POSTGRES_PASSWORD}

   database:
     - name: example_app
       create: true
       extensions:
         - name: pgcrypto

   users:
     - name: example_writer
       password: ${EXAMPLE_WRITER_PASSWORD}
       privileges:
         - db: example_app
           db_schema: public
           readwrite: true
           tables: [ALL]

   custom_sql:
     - name: seed_example_records
       database: example_app
       query: |
         INSERT INTO public.example_records (id, value)
         VALUES (%s, %s)
         ON CONFLICT (id) DO UPDATE SET value = EXCLUDED.value;
       params: [1, provisioned-by-cloudutil]
   ```

2. Reference it from a playbook (same directory), passing the schema by **path** and exporting any `${VAR}` values through `cloudutil_postgres_environment`:

   ```yaml
   # playbook.yml
   - name: Provision PostgreSQL from the CloudUtil schema
     hosts: localhost
     connection: local
     gather_facts: false
     vars:
       ansible_python_interpreter: "{{ ansible_playbook_python }}"
     roles:
       - role: cloudutil_postgres
         vars:
           cloudutil_postgres_config_file: "{{ playbook_dir }}/postgres.yaml"
           cloudutil_postgres_environment:
             POSTGRES_HOST: "{{ lookup('env', 'POSTGRES_HOST') }}"
             POSTGRES_USER: "{{ lookup('env', 'POSTGRES_USER') }}"
             POSTGRES_PASSWORD: "{{ vault_postgres_password }}"
             EXAMPLE_WRITER_PASSWORD: "{{ vault_example_writer_password }}"
   ```

3. Export whatever values you didn't pass through `cloudutil_postgres_environment`, then run it:

   ```bash
   export POSTGRES_HOST=localhost POSTGRES_USER=postgres
   ansible-playbook -i localhost, playbook.yml
   ```

`cloudutil_postgres_environment` is recommended for Ansible Vault values — it resolves `${VAR}` refs and is also visible as `{{ env.VAR }}` inside `custom_sql`. A fuller runnable sample lives at `examples/playbook.yml` + `examples/postgres.yaml`.

## Schemas

The schema is the Pydantic `SQLConfig` model in `roles/cloudutil_postgres/schemas/cloudutil_sql.py`. Annotated shape (comments are the field descriptions straight from the model):

```yaml
provider:
  name: string           # Identifier for the PostgreSQL provider/environment (e.g. 'primary', 'staging').
  version: string|int    # PostgreSQL server version reported by the provider.
  host: string           # Hostname or address of the PostgreSQL server; supports ${ENV_VAR} substitution.
  port: int              # TCP port the PostgreSQL server listens on.
  username: string       # Login username used to connect; supports ${ENV_VAR} substitution.
  password: string       # Login password used to connect; supports ${ENV_VAR} substitution.
  cert: string           # Optional CA certificate path for TLS verification; requires ssl_mode 'verify-ca'/'verify-full'.
  ssl_mode: string        # Optional: disable | allow | prefer | require | verify-ca | verify-full.
  mtls:                   # Optional mutual TLS client cert/key; not allowed when ssl_mode is 'disable'.
    cert: string          # Path to the client certificate file used for mutual TLS authentication.
    key: string           # Path to the client private key file used for mutual TLS authentication.

database:                 # List (or map keyed by name) of databases to manage.
  - name: string          # Name of the database to manage.
    create: true          # Whether to create the database if it does not already exist. Default: true.
    extensions:
      - name: string       # Name of the PostgreSQL extension to install (e.g. 'pgcrypto').

users:
  - name: string          # Username of the PostgreSQL role/user to create or manage.
    password: string      # Password for the user; supports ${ENV_VAR} substitution.
    privileges:
      - db: string          # Name of the database this privilege grant applies to.
        db_schema: public    # Schema within the database the grant targets. Default: 'public'.
        readwrite: false     # Grant read and write privileges. Mutually exclusive with readonly.
        readonly: false      # Grant read-only privileges. Mutually exclusive with readwrite.
        tables: []            # Table names to grant privileges on; use ['ALL'] for current + future tables.

custom_sql:
  - name: string          # Optional identifier for the query, used for logging/reference.
    database: postgres     # Name of the database the query runs against. Default: 'postgres'.
    query: string           # SQL query text, optionally with Jinja2 syntax, rendered before execution.
    template_context: {}    # Variables passed into the Jinja2 rendering context for the query.
    loader_path: "."        # Path(s) used to resolve {% include %} SQL files referenced by the query.
    inject_env: true         # Whether to expose the process environment as 'env' in the Jinja2 context.
    params: []                # Positional parameters bound to the SQL query for parameterized execution.
```

`${VAR}` on `provider`/`users` fields resolves from the process environment or `cloudutil_postgres_environment`. An inline `cloudutil_postgres_config` (instead of `_config_file`) needs `!unsafe` on any Jinja inside `custom_sql.query`, since Ansible would otherwise render it before the gateway does.

## Native module mapping

| Schema resource | Module |
| --- | --- |
| `database` | `community.postgresql.postgresql_db` |
| `extensions` | `community.postgresql.postgresql_ext` (`version: latest`) |
| `users` | `community.postgresql.postgresql_user` |
| `privileges` | `community.postgresql.postgresql_privs` |
| `custom_sql.query_raw` | `community.postgresql.postgresql_query` |

`tables: [ALL]` grants current-table privileges and `ALTER DEFAULT PRIVILEGES` for future tables. Default privileges belong to the connecting provider role, so create application tables with that same role. Full role contract: `roles/cloudutil_postgres/README.md`.

## Testing & CI

`task test` (or `bash test/run.sh`) builds a mock-RDS PostgreSQL container via Docker Compose, runs each `test/case/*/playbook.yml` against it, and validates the outcome with that case's `validate.sh`.

```bash
act workflow_dispatch -W .github/workflows/ansible.yml -j ansible
```

`.actrc` pins `catthehacker/ubuntu:act-latest` for `ubuntu-latest`. First `act` run downloads the runner, action deps, and the PostgreSQL image.

## TL;DR

An Ansible role that provisions PostgreSQL (RDS-compatible) from a validated YAML schema — Pydantic gateway validates/renders, native `community.postgresql` modules do the actual work. No dependency on the `cloudutil` Python package; ships as a standalone Docker image with pinned deps and its own end-to-end test suite.
