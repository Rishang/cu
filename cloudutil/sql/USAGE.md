# SQL Module Usage Guide

## Overview

Idempotent, type-safe database provisioning for PostgreSQL — databases, extensions, users, privileges, and optional custom SQL in one declarative YAML file.

---

## Quick Start

### 1. Create a configuration file

```yaml
provider:
  name: postgres
  version: 17
  host: localhost
  port: 5432
  username: postgres
  password: ${POSTGRES_PASSWORD}

database:
  - name: myapp
    create: true
    extensions:
      - name: uuid-ossp

users:
  - name: app_user
    password: ${APP_PASSWORD}
    privileges:
      - db: myapp
        db_schema: public
        readwrite: true
        tables:
          - ALL
```

### 2. Run it

```bash
export POSTGRES_PASSWORD=secret
export APP_PASSWORD=apppass

# Validate first (no DB connection)
cu sql validate config.yaml

# Apply
cu sql execute --config-file config.yaml
```

Or from Python:

```python
from cloudutil.sql.modules import PostgreSQLBuilder

provider = PostgreSQLBuilder().from_yaml("config.yaml").build()
with provider:
    provider.execute()
```

---

## Configuration Reference

### Provider

```yaml
provider:
  name: postgres
  version: 17
  host: localhost          # or ${DB_HOST}
  port: 5432
  username: postgres       # or ${POSTGRES_USER}
  password: ${POSTGRES_PASSWORD}

  # SSL — both fields are optional and independent
  ssl_mode: verify-full    # disable | allow | prefer | require | verify-ca | verify-full
  cert: /etc/ssl/pg-ca.crt # path to CA cert; requires ssl_mode verify-ca or verify-full
```

`username` and `password` support `${ENV_VAR}` substitution. `ssl_mode` is validated at config parse time — unknown values are rejected. If `cert` is set, `ssl_mode` must be `verify-ca` or `verify-full` (defaults to `verify-full` when `cert` is present and `ssl_mode` is not set).

### Databases

```yaml
database:
  - name: myapp
    create: true      # false = assume it already exists, skip CREATE
    extensions:
      - name: uuid-ossp
      - name: pgcrypto
```

Each database is checked before any action:
- **Not found** → `CREATE DATABASE`
- **Wrong owner** → `ALTER DATABASE … OWNER TO`
- **Already correct** → `SKIP`

Extensions follow the same pattern:
- **Not installed** → `CREATE EXTENSION IF NOT EXISTS`
- **Already installed** → `ALTER EXTENSION … UPDATE` (skips gracefully with a warning if the extension doesn't support updates)

### Users & Privileges

```yaml
users:
  - name: app_readwrite
    password: ${APP_RW_PASSWORD}
    privileges:
      - db: myapp
        db_schema: public
        readwrite: true        # SELECT, INSERT, UPDATE, DELETE + CREATE on schema
        tables:
          - ALL                # all current and future tables

  - name: app_readonly
    password: ${APP_RO_PASSWORD}
    privileges:
      - db: myapp
        db_schema: public
        readonly: true         # SELECT only
        tables:
          - users
          - orders
```

**Access modes:**

| `readwrite` | `readonly` | SQL granted |
|-------------|------------|-------------|
| `true` | `false` | `SELECT, INSERT, UPDATE, DELETE` + `CREATE ON SCHEMA` |
| `false` | `true` | `SELECT` |
| `false` | `false` | `GRANT CONNECT` + `GRANT USAGE` only (no table grants) |
| `true` | `true` | **Validation error** — mutually exclusive |

**`tables` values:**

| Value | Behaviour |
|-------|-----------|
| `[ALL]` | `GRANT … ON ALL TABLES` + `ALTER DEFAULT PRIVILEGES` (covers future tables) |
| `[users, orders, …]` | Per-table grants only |
| `[]` | `GRANT CONNECT` + `GRANT USAGE` on schema — no table-level permissions |

Users are created if missing or have their password updated if they already exist. All GRANTs are idempotent in Postgres (re-running is safe).

### Custom SQL

Optional arbitrary SQL that runs **after** databases, extensions, users, and privileges.

```yaml
custom_sql:
  # Plain DDL
  - name: migrations_table
    database: myapp
    query: |
      CREATE TABLE IF NOT EXISTS public._migrations (
        id SERIAL PRIMARY KEY,
        version TEXT NOT NULL UNIQUE,
        applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
      );

  # Parameterized DML — use params: for variable/untrusted input
  - name: seed_settings
    database: myapp
    query: INSERT INTO public.app_settings (key, value) VALUES (%s, %s) ON CONFLICT (key) DO NOTHING
    params:
      - maintenance_mode
      - "false"

  # Jinja template — interpolate template_context values
  - name: create_index
    database: myapp
    query: "CREATE INDEX IF NOT EXISTS idx_{{ table }}_id ON public.{{ table }} (id);"
    template_context:
      table: users

  # Environment variables via {{ env.VAR }} (inject_env: true by default)
  - name: record_version
    database: myapp
    query: |
      INSERT INTO public._migrations (version)
      VALUES ('{{ env.APP_VERSION }}')
      ON CONFLICT (version) DO NOTHING

  # Include external SQL files
  - name: from_file
    database: myapp
    loader_path: ./sql/fragments   # root for FileSystemLoader
    query: "{% include 'bootstrap.sql' %}"
```

**Fields:**

| Field | Default | Description |
|-------|---------|-------------|
| `query` | required | Jinja2 template string; rendered to `query_raw` at parse time |
| `database` | `postgres` | Target database (must exist or be created earlier in `database:`) |
| `params` | `[]` | Positional `%s` bind parameters |
| `template_context` | `{}` | Variables passed to `render()` |
| `loader_path` | `"."` | Directory (or list) for `FileSystemLoader` (`{% include %}` / `{% import %}`) |
| `inject_env` | `true` | Exposes `os.environ` as `{{ env.VAR }}` in templates |
| `name` | `null` | Label shown in logs and change reports |

---

## CLI Commands

```bash
# Generate a starter config
cu sql init
cu sql init -o my-config.yaml

# Validate without connecting
cu sql validate config.yaml

# Apply
cu sql execute --config-file config.yaml
cu sql execute -c config.yaml
```

---

## Ansible

Use the native `cloudutil_postgres` role in `ansible/roles/cloudutil_postgres`. It validates and renders this same Pydantic schema, then applies it with `community.postgresql` modules:

| Schema resource | Native module |
|---|---|
| databases | `community.postgresql.postgresql_db` |
| extensions | `community.postgresql.postgresql_ext` |
| users | `community.postgresql.postgresql_user` |
| privileges | `community.postgresql.postgresql_privs` |
| custom SQL | `community.postgresql.postgresql_query` |

Install its self-contained controller dependencies and pinned collection with UV:

```bash
cd ansible
uv sync --locked
uv run ansible-galaxy collection install -r requirements.yml -p collections --force
export ANSIBLE_ROLES_PATH="$PWD/roles${ANSIBLE_ROLES_PATH:+:$ANSIBLE_ROLES_PATH}"
export ANSIBLE_COLLECTIONS_PATH="$PWD/collections${ANSIBLE_COLLECTIONS_PATH:+:$ANSIBLE_COLLECTIONS_PATH}"
```

Pass the YAML path to the role rather than loading it with `vars_files`; this ensures `custom_sql.query` keeps its Pydantic/Jinja rendering semantics:

```yaml
- name: Provision PostgreSQL
  hosts: localhost
  connection: local
  gather_facts: false
  roles:
    - role: cloudutil_postgres
      vars:
        cloudutil_postgres_config_file: "{{ playbook_dir }}/postgres.yaml"
        cloudutil_postgres_environment:
          POSTGRES_PASSWORD: "{{ vault_postgres_password }}"
          APP_SERVICE_PASSWORD: "{{ vault_app_password }}"
```

`${VAR}` substitutions and `{{ env.VAR }}` in `custom_sql` read the inherited process environment plus `cloudutil_postgres_environment`. The latter is the recommended way to pass Ansible Vault values. The managed host needs `psycopg2` or `psycopg`, as required by the `community.postgresql` modules.

For inline input, set `cloudutil_postgres_config` instead. Mark SQL Jinja as `!unsafe` so Ansible does not render it before `SQLConfig`:

```yaml
cloudutil_postgres_config:
  custom_sql:
    - database: myapp
      query: !unsafe "CREATE INDEX idx_{{ table }} ON public.{{ table }} (id)"
      template_context:
        table: users
```

See [`ansible/roles/cloudutil_postgres/README.md`](../../ansible/roles/cloudutil_postgres/README.md) for complete role instructions.

---

## Features

- **Check-first / idempotent** — verifies current state before issuing any SQL; safe to re-run
- **Change tracking** — every operation emits a structured `ChangeReport` (`create` / `update` / `skip` / `execute`)
- **Environment variable substitution** — `${VAR}` in `username`, `password`, and custom SQL templates
- **SSL control** — explicit `ssl_mode` field plus optional CA cert path
- **Jinja2 custom SQL** — template rendering, file includes, env injection, bind params
- **Conflict validation** — `readwrite` and `readonly` cannot both be true; `ssl_mode` values are validated at parse time

---

## Example Output

```
============================================================
Databases
============================================================
[CREATE] database: myapp
[SKIP] database: analytics
[UPDATE] database: legacy (owner: old_admin → postgres)

============================================================
Extensions
============================================================
[CREATE] extension: myapp.uuid-ossp
[UPDATE] extension: myapp.pgcrypto
[SKIP] extension: myapp.pg_trgm

============================================================
Users
============================================================
[CREATE] user: app_service
[UPDATE] user: reports_user (password: *** → ***)

============================================================
Privileges
============================================================
[CREATE] privilege: app_service@myapp.public (access: READ/WRITE (ALL))
[CREATE] privilege: reports_user@myapp.public (access: READ-ONLY (4 tables))

============================================================
Custom SQL
============================================================
[EXECUTE] custom_sql: myapp/migrations_table (rowcount: -1)

============================================================
Summary
============================================================
Total: 8 | Created: 4 | Updated: 2 | Skipped: 1 | Executed: 1
Complete
```

---

## Kubernetes

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: sql-setup
spec:
  template:
    spec:
      containers:
        - name: sql-setup
          image: cloudutil:latest
          command: ["cu", "sql", "execute", "--config-file", "/config/config.yaml"]
          env:
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: sql-secrets
                  key: POSTGRES_PASSWORD
            - name: APP_SERVICE_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: sql-secrets
                  key: APP_SERVICE_PASSWORD
          volumeMounts:
            - name: config
              mountPath: /config
      volumes:
        - name: config
          configMap:
            name: sql-config
      restartPolicy: Never
```

---

## See Also

- `example.yaml` — comprehensive configuration with all features
- `modules/base.py` — Pydantic schema definitions (`ProviderConfig`, `DatabaseConfig`, `UserConfig`, `PrivilegeConfig`, `CustomSQLQuery`, `SQLConfig`)
- `modules/postgres.py` — PostgreSQL provider and builder
- `../../ansible/roles/cloudutil_postgres/` — native Ansible role implementation
