# cloudutil_postgres role

Applies the existing CloudUtil PostgreSQL YAML schema with native `community.postgresql` modules. The role owns its Pydantic gateway, so it does not import the `cloudutil` package. `${ENV_VAR}`, SSL validation, privilege validation, `template_context`, Jinja `env`, SQL-file includes, and positional query parameters retain their existing behavior.

## Table of Contents

- [Requirements](#requirements)
- [Use the unchanged schema](#use-the-unchanged-schema)
- [Schema reference](#schema-reference)
  - [`provider` (`ProviderConfig`)](#provider-providerconfig)
  - [`provider.mtls` (`MtlsConfig`)](#providermtls-mtlsconfig)
  - [`database` (`DatabaseConfig`)](#database-databaseconfig)
  - [`database[].extensions` (`ExtensionConfig`)](#databaseextensions-extensionconfig)
  - [`users` (`UserConfig`)](#users-userconfig)
  - [`users[].privileges` (`PrivilegeConfig`)](#usersprivileges-privilegeconfig)
  - [`custom_sql` (`CustomSQLQuery`)](#custom_sql-customsqlquery)
- [Native module mapping](#native-module-mapping)

## Requirements

Install the pinned controller dependencies and collection:

```bash
cd ansible
uv sync --locked
uv run ansible-galaxy collection install -r requirements.yml -p collections --force
export ANSIBLE_ROLES_PATH="$PWD/roles${ANSIBLE_ROLES_PATH:+:$ANSIBLE_ROLES_PATH}"
export ANSIBLE_COLLECTIONS_PATH="$PWD/collections${ANSIBLE_COLLECTIONS_PATH:+:$ANSIBLE_COLLECTIONS_PATH}"
```

The managed host must have `psycopg2` or `psycopg`, because that is where `community.postgresql` runs. Use `hosts: localhost` and `connection: local` when connecting to a remote PostgreSQL server from the controller. Alternatively, build the standalone controller image with `docker build -f ansible/Dockerfile -t cloudutil-postgres-ansible .`.

## Use the unchanged schema

Pass the schema path to the role—do **not** use `vars_files`. This prevents Ansible from rendering `custom_sql.query` before the role can render it with its own `template_context`, `loader_path`, and `env` behavior.

```yaml
# playbook.yml
- name: Configure PostgreSQL
  hosts: localhost
  connection: local
  gather_facts: false
  roles:
    - role: cloudutil_postgres
      vars:
        cloudutil_postgres_config_file: "{{ playbook_dir }}/postgres.yaml"
```

Run it with values exported for `${...}` references:

```bash
export DB_HOST=localhost POSTGRES_USER=postgres POSTGRES_PASSWORD=secret
export APP_SERVICE_PASSWORD=secret ANALYTICS_PASSWORD=secret
export AUDIT_PASSWORD=secret REPORTS_PASSWORD=secret APP_VERSION=1.2.3
ansible-playbook -i localhost, playbook.yml
```

For Ansible Vault or other Ansible variables, supply the values to the schema renderer instead of exporting them:

```yaml
roles:
  - role: cloudutil_postgres
    vars:
      cloudutil_postgres_config_file: "{{ playbook_dir }}/postgres.yaml"
      cloudutil_postgres_environment:
        POSTGRES_PASSWORD: "{{ vault_postgres_password }}"
        APP_SERVICE_PASSWORD: "{{ vault_app_service_password }}"
        APP_VERSION: "{{ app_version }}"
```

An inline schema is supported through `cloudutil_postgres_config`, but any SQL Jinja must be protected from Ansible with `!unsafe`:

```yaml
cloudutil_postgres_config:
  custom_sql:
    - database: myapp
      query: !unsafe "CREATE INDEX idx_{{ table }} ON public.{{ table }} (id)"
      template_context:
        table: users
```

## Schema reference

The schema gateway (`schemas/cloudutil_sql.py`) defines every accepted field as a Pydantic model with a `description`; the table below mirrors those descriptions so the contract is visible without reading the source. No separate `docs/` directory exists in this project — schema documentation lives here and in the field descriptions themselves.

### `provider` (`ProviderConfig`)

| Field | Description |
| --- | --- |
| `name` | Identifier for the PostgreSQL provider/environment (e.g. 'primary', 'staging'). |
| `version` | PostgreSQL server version reported by the provider. |
| `host` | Hostname or address of the PostgreSQL server; supports `${ENV_VAR}` substitution. |
| `port` | TCP port the PostgreSQL server listens on. |
| `username` | Login username used to connect to the PostgreSQL server; supports `${ENV_VAR}` substitution. |
| `password` | Login password used to connect to the PostgreSQL server; supports `${ENV_VAR}` substitution. |
| `cert` | Optional path to a server CA certificate for TLS verification; requires `ssl_mode` `verify-ca` or `verify-full`. |
| `mtls` | Optional mutual TLS client certificate/key configuration (see `MtlsConfig`); not allowed when `ssl_mode` is `disable`. |
| `ssl_mode` | SSL negotiation mode for the connection. One of: `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`. |

### `provider.mtls` (`MtlsConfig`)

| Field | Description |
| --- | --- |
| `cert` | Path to the client certificate file used for mutual TLS authentication. |
| `key` | Path to the client private key file used for mutual TLS authentication. |

### `database` (`DatabaseConfig`)

| Field | Description |
| --- | --- |
| `name` | Name of the database to manage. |
| `create` | Whether to create the database if it does not already exist. |
| `extensions` | Extensions to install on this database (see `ExtensionConfig`). |

### `database[].extensions` (`ExtensionConfig`)

| Field | Description |
| --- | --- |
| `name` | Name of the PostgreSQL extension to install (e.g. `pgcrypto`). |

### `users` (`UserConfig`)

| Field | Description |
| --- | --- |
| `name` | Username of the PostgreSQL role/user to create or manage. |
| `password` | Password for the user; supports `${ENV_VAR}` substitution. |
| `privileges` | Privilege grants assigned to this user (see `PrivilegeConfig`). |

### `users[].privileges` (`PrivilegeConfig`)

| Field | Description |
| --- | --- |
| `db` | Name of the database this privilege grant applies to. |
| `db_schema` | Schema within the database that the privilege grant targets. Defaults to `public`. |
| `readwrite` | Grant read and write privileges. Mutually exclusive with `readonly`. |
| `readonly` | Grant read-only privileges. Mutually exclusive with `readwrite`. |
| `tables` | Table names to grant privileges on; use `['ALL']` to include current and future tables. |

### `custom_sql` (`CustomSQLQuery`)

| Field | Description |
| --- | --- |
| `query` | SQL query text, optionally containing Jinja2 template syntax rendered before execution. |
| `query_raw` | Rendered SQL text populated automatically after Jinja2 rendering; not intended to be set by the caller. |
| `template_context` | Variables passed into the Jinja2 rendering context for the query. |
| `loader_path` | Filesystem path(s) used to resolve SQL file includes referenced by the Jinja2 template. |
| `inject_env` | Whether to expose the process environment as `env` inside the Jinja2 template context. |
| `database` | Name of the database the query runs against. Defaults to `postgres`. |
| `params` | Positional parameters bound to the SQL query for parameterized execution. |
| `name` | Optional identifier for the query, used for logging/reference. |

## Native module mapping

| Schema resource | Module |
| --- | --- |
| `database` | `community.postgresql.postgresql_db` |
| `extensions` | `community.postgresql.postgresql_ext` (`version: latest`) |
| `users` | `community.postgresql.postgresql_user` |
| `privileges` | `community.postgresql.postgresql_privs` |
| `custom_sql.query_raw` | `community.postgresql.postgresql_query` |

For `tables: [ALL]`, the role grants privileges to current tables and applies PostgreSQL default privileges for future tables, as the Python provider did. Default privileges belong to the configured provider/login role; create application tables with that role (or add an explicit owner field later).

`custom_sql` remains caller-defined SQL and runs on every normal play. Make DDL/DML idempotent where required.
