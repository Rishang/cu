from __future__ import annotations

import traceback
from collections import Counter
from typing import Any

from ansible.module_utils.basic import AnsibleModule

try:
    from cloudutil.sql.modules.postgres import PostgreSQLBuilder

    HAS_DEPS = True
except ImportError:
    HAS_DEPS = False


DOCUMENTATION = r"""
---
module: cloudutil_postgres
short_description: Provision PostgreSQL databases, extensions, users, and privileges
description:
  - Idempotent provisioning of PostgreSQL resources from a config dict, YAML file, or YAML string.
options:
  config:
    description: Inline config dict (mutually exclusive with config_file and config_string).
    type: dict
  config_file:
    description: Path to a YAML config file on the target host.
    type: path
  config_string:
    description: Raw YAML string.
    type: str
requirements:
  - psycopg2
  - cloudutil
"""

EXAMPLES = r"""
- name: Provision from inline config
  cloudutil_postgres:
    config:
      provider:
        name: postgres
        version: 17
        host: localhost
        port: 5432
        username: postgres
        password: "{{ pg_password }}"
      database:
        - name: myapp
          create: true
          extensions:
            - name: uuid-ossp
      users:
        - name: app_user
          password: "{{ app_password }}"
          privileges:
            - db: myapp
              readwrite: true
              tables: [ALL]

- name: Provision from YAML file
  cloudutil_postgres:
    config_file: /etc/myapp/pg_config.yaml

- name: Provision from YAML string
  cloudutil_postgres:
    config_string: "{{ lookup('file', 'pg_config.yaml') }}"
"""

RETURN = r"""
changed:
  description: True if any resource was created, updated, or executed.
  returned: always
  type: bool
changes:
  description: List of all operations performed.
  returned: always
  type: list
  elements: dict
summary:
  description: Count of each operation type.
  returned: always
  type: dict
  sample: {total: 4, create: 2, update: 1, skip: 1, execute: 0}
"""


def _build_provider(params: dict[str, Any]):
    match params:
        case {"config": config} if config:
            return PostgreSQLBuilder().from_dict(config).build()
        case {"config_file": path} if path:
            return PostgreSQLBuilder().from_yaml(path).build()
        case {"config_string": s} if s:
            return PostgreSQLBuilder().from_yaml_string(s).build()
        case _:
            raise ValueError(
                "No valid config source provided (config, config_file, or config_string)"
            )


def main() -> None:
    module = AnsibleModule(
        argument_spec={
            "config": {"type": "dict"},
            "config_file": {"type": "path"},
            "config_string": {"type": "str"},
        },
        mutually_exclusive=[["config", "config_file", "config_string"]],
        required_one_of=[["config", "config_file", "config_string"]],
        supports_check_mode=False,
    )

    if not HAS_DEPS:
        module.fail_json(
            msg="Missing required dependencies: psycopg2, cloudutil. "
            "Install with: pip install psycopg2-binary cloudutil"
        )

    try:
        provider = _build_provider(module.params)
        with provider:
            provider.execute()
    except Exception:
        module.fail_json(msg=traceback.format_exc())

    changes = [c.model_dump(exclude_none=True) for c in provider.changes]
    counts = Counter(c["operation"] for c in changes)

    module.exit_json(
        changed=any(c["operation"] in ("create", "update", "execute") for c in changes),
        changes=changes,
        summary={
            "total": len(changes),
            "create": counts["create"],
            "update": counts["update"],
            "skip": counts["skip"],
            "execute": counts["execute"],
        },
    )


if __name__ == "__main__":
    main()
