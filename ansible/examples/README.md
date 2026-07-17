# PostgreSQL role example

`playbook.yml` applies the complete `postgres.yaml` schema through the self-contained `cloudutil_postgres` role. It creates `example_app`, enables `pgcrypto`, creates reader/writer/login-only roles, grants current and future-table access, renders an included SQL template with `{{ env.EXAMPLE_APP_VERSION }}`, executes parameterized SQL, and demonstrates `inject_env: false`.

From `ansible/`, supply values and run it:

```bash
export POSTGRES_HOST=127.0.0.1
export POSTGRES_USER=postgres
export POSTGRES_PASSWORD=postgres
export EXAMPLE_READER_PASSWORD=change-reader
export EXAMPLE_WRITER_PASSWORD=change-writer
export EXAMPLE_AUTOMATION_PASSWORD=change-automation
export EXAMPLE_APP_VERSION=1.0.0
uv run ansible-playbook -i localhost, examples/playbook.yml
```

Set `ANSIBLE_ROLES_PATH` and `ANSIBLE_COLLECTIONS_PATH` as described in `../README.md`. `postgres.yaml` uses `ssl_mode: prefer` for local compatibility; for CA validation, replace it with `ssl_mode: verify-full` and set `cert` to a readable CA certificate path. Named-table privileges are also supported, but the named tables must already exist when the role runs; use `tables: [ALL]` for tables created by `custom_sql` in the same role execution.
