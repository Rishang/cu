"""Apply PostgreSQL configuration — used by the CLI."""

from pathlib import Path
from typing import Any

from cloudutil.sql.modules.postgres import PostgreSQLBuilder

CHANGE_OPS = {"create", "update", "execute"}


def apply_postgres_config(config_path: str | Path) -> tuple[bool, list[dict[str, Any]]]:
    """Apply a PostgreSQL YAML config file. Returns (changed, change reports)."""
    provider = PostgreSQLBuilder().from_yaml(config_path).build()
    with provider:
        provider.execute()

    changed = any(c.operation in CHANGE_OPS for c in provider.changes)
    return changed, [c.model_dump() for c in provider.changes]


def validate_postgres_config(config_path: str | Path) -> None:
    """Parse and validate a config file without connecting to the database."""
    PostgreSQLBuilder().from_yaml(config_path)
