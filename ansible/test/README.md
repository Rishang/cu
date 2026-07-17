# Role gateway and mock-RDS tests

Run all Ansible tests from this directory with the locked UV environment:

```bash
cd ansible
uv sync --locked
uv run ansible-galaxy collection install -r requirements.yml -p collections --force
ANSIBLE_ROLES_PATH="$PWD/roles" \
ANSIBLE_COLLECTIONS_PATH="$PWD/collections" \
  uv run pytest -q test
uv run bash test/run-rds-mock.sh
```

The focused tests load the Ansible filter adapter and its `schemas/cloudutil_sql.py` gateway directly. `rds-mock/` builds `postgres:18-alpine` with an initialization script that creates an AWS-RDS-like `rds_master_user`: it is `NOSUPERUSER`, but has `CREATEDB` and `CREATEROLE`. The scenario verifies database ownership, extension installation, rendered and parameterized SQL, current/future `tables: [ALL]` grants, and reader/writer login permissions. The Compose project and volume are removed automatically.
