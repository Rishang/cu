from collections import Counter
from contextlib import contextmanager
from pathlib import Path
from typing import Any, Literal

import psycopg2
import yaml
from psycopg2 import sql
from pydantic import BaseModel

from cloudutil.utils import logger
from .base import (
    BaseSQLProvider,
    CustomSQLQuery,
    DatabaseConfig,
    ExtensionConfig,
    PrivilegeConfig,
    SQLConfig,
    UserConfig,
)


class ChangeReport(BaseModel):
    """Tracks what changed during execution."""

    operation: Literal["create", "update", "skip", "execute"]
    resource_type: str
    resource_name: str
    details: dict[str, Any] | None = None

    def __str__(self) -> str:
        base = f"[{self.operation.upper()}] {self.resource_type}: {self.resource_name}"
        if not self.details:
            return base
        parts = []
        for k, v in self.details.items():
            if isinstance(v, dict) and "old" in v and "new" in v:
                parts.append(f"{k}: {v['old']} → {v['new']}")
            else:
                parts.append(f"{k}: {v}")
        return f"{base} ({', '.join(parts)})"


class PostgreSQLProvider(BaseSQLProvider):
    """PostgreSQL provider — idempotent, check-first."""

    def __init__(self, config: SQLConfig):
        super().__init__(config)
        self._conn = None
        self.conn_params: dict = {}
        self.changes: list[ChangeReport] = []

    def connect(self) -> None:
        p = self.config.provider
        self.conn_params = {
            "host": p.host,
            "port": p.port,
            "user": p.username,
            "password": p.password,
        }
        if p.ssl_mode:
            self.conn_params["sslmode"] = p.ssl_mode
        if p.cert:
            self.conn_params.setdefault("sslmode", "verify-full")
            self.conn_params["sslrootcert"] = p.cert

        self._conn = psycopg2.connect(**self.conn_params, database="postgres")
        self._conn.autocommit = True
        logger.info(f"Connected to {p.host}:{p.port}")

    def disconnect(self) -> None:
        if self._conn:
            self._conn.close()
            self._conn = None

    @contextmanager
    def _cursor(self, db: str = "postgres"):
        """Yield a cursor — reuses main connection for 'postgres', opens a
        short-lived connection for any other database."""
        if db == "postgres":
            with self._conn.cursor() as cur:
                yield cur
        else:
            conn = psycopg2.connect(**self.conn_params, database=db)
            conn.autocommit = True
            try:
                with conn.cursor() as cur:
                    yield cur
            finally:
                conn.close()

    def _log(
        self,
        operation: str,
        resource_type: str,
        resource_name: str,
        details: dict | None = None,
    ) -> None:
        change = ChangeReport(
            operation=operation,
            resource_type=resource_type,
            resource_name=resource_name,
            details=details,
        )
        self.changes.append(change)
        logger.info(str(change))

    def _section(self, name: str) -> None:
        logger.info(f"\n{'=' * 60}\n{name}\n{'=' * 60}")

    def create_database(self, db_config: DatabaseConfig) -> None:
        if not db_config.create:
            return

        name = db_config.name
        owner = self.config.provider.username
        db_id, owner_id = sql.Identifier(name), sql.Identifier(owner)

        with self._cursor() as cur:
            cur.execute(
                "SELECT rolname FROM pg_roles WHERE oid = "
                "(SELECT datdba FROM pg_database WHERE datname = %s)",
                (name,),
            )
            row = cur.fetchone()

            if row is None:
                cur.execute(
                    sql.SQL("CREATE DATABASE {} OWNER {}").format(db_id, owner_id)
                )
                self._log("create", "database", name)
            elif row[0] != owner:
                cur.execute(
                    sql.SQL("ALTER DATABASE {} OWNER TO {}").format(db_id, owner_id)
                )
                self._log(
                    "update", "database", name, {"owner": {"old": row[0], "new": owner}}
                )
            else:
                self._log("skip", "database", name)

    def install_extensions(
        self, db_name: str, extensions: list[ExtensionConfig]
    ) -> None:
        for ext in extensions:
            ext_id = sql.Identifier(ext.name)
            label = f"{db_name}.{ext.name}"
            with self._cursor(db_name) as cur:
                cur.execute(
                    "SELECT 1 FROM pg_extension WHERE extname = %s", (ext.name,)
                )
                if cur.fetchone():
                    try:
                        cur.execute(sql.SQL("ALTER EXTENSION {} UPDATE").format(ext_id))
                        self._log("update", "extension", label)
                    except Exception as e:
                        logger.warning(
                            f"ALTER EXTENSION {ext.name} UPDATE failed, skipping: {e}"
                        )
                        self._log("skip", "extension", label)
                else:
                    cur.execute(
                        sql.SQL("CREATE EXTENSION IF NOT EXISTS {}").format(ext_id)
                    )
                    self._log("create", "extension", label)

    def create_user(self, user_config: UserConfig) -> None:
        with self._cursor() as cur:
            cur.execute(
                "SELECT 1 FROM pg_roles WHERE rolname = %s", (user_config.name,)
            )
            exists = cur.fetchone() is not None
            verb = "ALTER USER" if exists else "CREATE USER"
            cur.execute(
                sql.SQL(f"{verb} {{}} WITH PASSWORD %s").format(
                    sql.Identifier(user_config.name)
                ),
                (user_config.password,),
            )

        op = "update" if exists else "create"
        details = {"password": {"old": "***", "new": "***"}} if exists else None
        self._log(op, "user", user_config.name, details)

    def grant_privileges(self, user_name: str, priv: PrivilegeConfig) -> None:
        label = f"{user_name}@{priv.db}.{priv.db_schema}"
        if not priv.readwrite and not priv.readonly:
            self._log("skip", "privilege", label)
            return

        privs = "SELECT, INSERT, UPDATE, DELETE" if priv.readwrite else "SELECT"
        mode = "READ/WRITE" if priv.readwrite else "READ-ONLY"
        schema, user = sql.Identifier(priv.db_schema), sql.Identifier(user_name)

        with self._cursor(priv.db) as cur:
            cur.execute(
                sql.SQL("GRANT CONNECT ON DATABASE {} TO {}").format(
                    sql.Identifier(priv.db), user
                )
            )
            cur.execute(sql.SQL("GRANT USAGE ON SCHEMA {} TO {}").format(schema, user))
            if priv.readwrite:
                cur.execute(
                    sql.SQL("GRANT CREATE ON SCHEMA {} TO {}").format(schema, user)
                )

            if not priv.tables:
                access = "CONNECT+USAGE only (no table grants)"
            elif "ALL" in priv.tables:
                cur.execute(
                    sql.SQL(
                        f"GRANT {privs} ON ALL TABLES IN SCHEMA {{}} TO {{}}"
                    ).format(schema, user)
                )
                cur.execute(
                    sql.SQL(
                        f"ALTER DEFAULT PRIVILEGES IN SCHEMA {{}} GRANT {privs} ON TABLES TO {{}}"
                    ).format(schema, user)
                )
                access = f"{mode} (ALL)"
            else:
                for table in priv.tables:
                    cur.execute(
                        sql.SQL(f"GRANT {privs} ON TABLE {{}}.{{}} TO {{}}").format(
                            schema, sql.Identifier(table), user
                        )
                    )
                access = f"{mode} ({len(priv.tables)} tables)"

        self._log("create", "privilege", label, {"access": access})

    def execute_custom_sql(self, item: CustomSQLQuery, index: int) -> None:
        label = item.name or f"query[{index}]"
        with self._cursor(item.database) as cur:
            cur.execute(item.query_raw, tuple(item.params) or None)
            self._log(
                "execute",
                "custom_sql",
                f"{item.database}/{label}",
                {"rowcount": cur.rowcount},
            )

    def execute(self) -> None:
        self.changes = []
        logger.info("Starting PostgreSQL configuration")

        self._section("Databases")
        for db in self.config.database.values():
            self.create_database(db)

        self._section("Extensions")
        for db in self.config.database.values():
            if db.extensions:
                self.install_extensions(db.name, db.extensions)

        self._section("Users")
        for user in self.config.users:
            self.create_user(user)

        self._section("Privileges")
        for user in self.config.users:
            for priv in user.privileges:
                self.grant_privileges(user.name, priv)

        if self.config.custom_sql:
            self._section("Custom SQL")
            for idx, item in enumerate(self.config.custom_sql):
                self.execute_custom_sql(item, idx)

        counts = Counter(c.operation for c in self.changes)
        self._section("Summary")
        logger.info(
            f"Total: {len(self.changes)} | Created: {counts['create']} | "
            f"Updated: {counts['update']} | Skipped: {counts['skip']} | Executed: {counts['execute']}"
        )
        logger.info("Complete")


class PostgreSQLBuilder:
    """Fluent builder — load config from dict, YAML file, or YAML string."""

    def __init__(self):
        self._config: SQLConfig | None = None

    def from_dict(self, config_dict: dict) -> "PostgreSQLBuilder":
        self._config = SQLConfig(**config_dict)
        return self

    def from_yaml(self, yaml_path: str | Path) -> "PostgreSQLBuilder":
        path = Path(yaml_path).expanduser()
        if not path.is_file():
            raise FileNotFoundError(f"YAML file not found: {path}")
        return self.from_yaml_string(path.read_text())

    def from_yaml_string(self, yaml_string: str) -> "PostgreSQLBuilder":
        data = yaml.safe_load(yaml_string)
        if not isinstance(data, dict):
            raise ValueError("YAML input is empty or not a mapping")
        return self.from_dict(data)

    def build(self) -> PostgreSQLProvider:
        if self._config is None:
            raise ValueError("Configuration not set. Call from_dict/from_yaml first.")
        return PostgreSQLProvider(self._config)
