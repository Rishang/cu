# Ansible: CloudUtil PostgreSQL module

This repository ships an Ansible-compatible module that applies the same PostgreSQL configuration as `cu sql execute`.

## Requirements

- **Ansible** on the machine that runs the module (usually the controller).
- **`cloudutil` installed in the Python environment Ansible uses** (e.g. `pip install cloudutil` or `uv pip install -e .` from a checkout).

The module imports `cloudutil.sql.apply` and connects with `psycopg2`. Run it on a host where that environment is available—typically **`delegate_to: localhost`** with **`connection: local`**.

## Install the module for Ansible

Point Ansible’s module search path at the directory that contains `cloudutil_postgres.py`:

```ini
# ansible.cfg (next to your playbook)
[defaults]
library = ../cloudutil/sql/ansible
```

Or copy/symlink `cloudutil/sql/ansible/cloudutil_postgres.py` into your playbook’s `library/` directory.

## Example playbook

```yaml
- name: Apply CloudUtil PostgreSQL config
  hosts: localhost
  connection: local
  gather_facts: false
  tasks:
    - name: Provision DB objects from YAML
      cloudutil_postgres:
        config_path: "{{ playbook_dir }}/../cloudutil/sql/example.yaml"
        state: present
      environment:
        POSTGRES_PASSWORD: "{{ lookup('env', 'POSTGRES_PASSWORD') }}"
```

Use `state: validated` to only parse/validate the YAML (no database connection).

## Module reference

See `DOCUMENTATION` / `EXAMPLES` in `cloudutil/sql/ansible/cloudutil_postgres.py`, and `cloudutil/sql/USAGE.md` for the YAML schema.
