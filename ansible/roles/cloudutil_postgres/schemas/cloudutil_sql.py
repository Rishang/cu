"""Self-contained Pydantic gateway for the CloudUtil PostgreSQL Ansible role."""

from __future__ import annotations

import os
from collections.abc import Mapping
from contextlib import contextmanager
from pathlib import Path
from typing import Any, cast

import yaml
from jinja2 import Environment, FileSystemLoader
from pydantic import BaseModel, Field, field_validator, model_validator

SSL_MODES = {"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
SSL_VERIFY_MODES = {"verify-ca", "verify-full"}


def _resolve_env_variable(value: str, field_name: str) -> str:
    if isinstance(value, str) and value.startswith("${") and value.endswith("}"):
        name = value[2:-1]
        if (resolved := os.getenv(name)) is None:
            raise ValueError(
                f"Environment variable '{name}' for {field_name} is not set"
            )
        return resolved
    return value


class MtlsConfig(BaseModel):
    cert: str = Field(
        description="Path to the client certificate file used for mutual TLS authentication."
    )
    key: str = Field(
        description="Path to the client private key file used for mutual TLS authentication."
    )


class ProviderConfig(BaseModel):
    name: str = Field(
        description="Identifier for the PostgreSQL provider/environment (e.g. 'primary', 'staging')."
    )
    version: str | int = Field(
        description="PostgreSQL server version reported by the provider."
    )
    host: str = Field(
        description="Hostname or address of the PostgreSQL server; supports ${ENV_VAR} substitution."
    )
    port: int = Field(description="TCP port the PostgreSQL server listens on.")
    username: str = Field(
        description="Login username used to connect to the PostgreSQL server; supports ${ENV_VAR} substitution."
    )
    password: str = Field(
        description="Login password used to connect to the PostgreSQL server; supports ${ENV_VAR} substitution."
    )
    cert: str | None = Field(
        default=None,
        description="Optional path to a server CA certificate for TLS verification; requires ssl_mode 'verify-ca' or 'verify-full'.",
    )
    mtls: MtlsConfig | None = Field(
        default=None,
        description="Optional mutual TLS client certificate/key configuration; not allowed when ssl_mode is 'disable'.",
    )
    ssl_mode: str | None = Field(
        default=None,
        description="SSL negotiation mode for the connection. One of: disable, allow, prefer, require, verify-ca, verify-full.",
    )

    @field_validator("host", "username", "password", mode="before")
    @classmethod
    def resolve_env_vars(cls, value: str, info: Any) -> str:
        return _resolve_env_variable(value, f"provider.{info.field_name}")

    @model_validator(mode="after")
    def validate_ssl(self) -> ProviderConfig:
        if self.ssl_mode and self.ssl_mode not in SSL_MODES:
            raise ValueError(
                f"provider.ssl_mode '{self.ssl_mode}' is not valid. "
                f"Choose from: {', '.join(sorted(SSL_MODES))}"
            )
        if self.cert and self.ssl_mode and self.ssl_mode not in SSL_VERIFY_MODES:
            raise ValueError(
                "provider.cert requires ssl_mode 'verify-ca' or 'verify-full', "
                f"got '{self.ssl_mode}'"
            )
        if self.mtls and self.ssl_mode == "disable":
            raise ValueError("provider.mtls cannot be used when ssl_mode is 'disable'")
        return self


class ExtensionConfig(BaseModel):
    name: str = Field(
        description="Name of the PostgreSQL extension to install (e.g. 'pgcrypto')."
    )


class DatabaseConfig(BaseModel):
    name: str = Field(description="Name of the database to manage.")
    create: bool = Field(
        default=True,
        description="Whether to create the database if it does not already exist.",
    )
    extensions: list[ExtensionConfig] = Field(
        default_factory=list, description="Extensions to install on this database."
    )


class PrivilegeConfig(BaseModel):
    db: str = Field(description="Name of the database this privilege grant applies to.")
    db_schema: str = Field(
        default="public",
        description="Schema within the database that the privilege grant targets.",
    )
    readwrite: bool = Field(
        default=False,
        description="Grant read and write privileges. Mutually exclusive with readonly.",
    )
    readonly: bool = Field(
        default=False,
        description="Grant read-only privileges. Mutually exclusive with readwrite.",
    )
    tables: list[str] = Field(
        default_factory=list,
        description="Table names to grant privileges on; use ['ALL'] to include current and future tables.",
    )

    @model_validator(mode="after")
    def validate_access_flags(self) -> PrivilegeConfig:
        if self.readwrite and self.readonly:
            raise ValueError(
                f"privilege for db '{self.db}': readwrite and readonly cannot both be true"
            )
        return self


class UserConfig(BaseModel):
    name: str = Field(
        description="Username of the PostgreSQL role/user to create or manage."
    )
    password: str = Field(
        description="Password for the user; supports ${ENV_VAR} substitution."
    )
    privileges: list[PrivilegeConfig] = Field(
        default_factory=list, description="Privilege grants assigned to this user."
    )

    @field_validator("password", mode="before")
    @classmethod
    def resolve_password(cls, value: str) -> str:
        return _resolve_env_variable(value, "user.password")


class CustomSQLQuery(BaseModel):
    query: str = Field(
        description="SQL query text, optionally containing Jinja2 template syntax rendered before execution."
    )
    query_raw: str = Field(
        default="",
        description="Rendered SQL text populated automatically after Jinja2 rendering; not intended to be set by the caller.",
    )
    template_context: dict[str, Any] = Field(
        default_factory=dict,
        description="Variables passed into the Jinja2 rendering context for the query.",
    )
    loader_path: str | list[str] = Field(
        default=".",
        description="Filesystem path(s) used to resolve SQL file includes referenced by the Jinja2 template.",
    )
    inject_env: bool = Field(
        default=True,
        description="Whether to expose the process environment as 'env' inside the Jinja2 template context.",
    )
    database: str = Field(
        default="postgres", description="Name of the database the query runs against."
    )
    params: list[Any] = Field(
        default_factory=list,
        description="Positional parameters bound to the SQL query for parameterized execution.",
    )
    name: str | None = Field(
        default=None,
        description="Optional identifier for the query, used for logging/reference.",
    )

    @field_validator("query", "database", mode="before")
    @classmethod
    def nonblank(cls, value: Any, info: Any) -> str:
        if isinstance(value, str) and value.strip():
            return value.strip()
        raise ValueError(f"custom_sql.{info.field_name} must be a non-empty string")

    def model_post_init(self, __context: Any) -> None:
        paths = (
            [self.loader_path]
            if isinstance(self.loader_path, str)
            else self.loader_path
        )
        resolved = []
        for path in paths:
            directory = Path(path).expanduser().resolve()
            if not directory.is_dir():
                raise ValueError(
                    f"custom_sql.loader_path must be an existing directory: {directory}"
                )
            resolved.append(str(directory))

        environment = Environment(loader=FileSystemLoader(resolved), autoescape=False)
        if self.inject_env:
            cast(dict[str, Any], environment.globals)["env"] = os.environ
        self.query_raw = environment.from_string(self.query).render(
            **self.template_context
        )


class SQLConfig(BaseModel):
    provider: ProviderConfig = Field(
        description="Connection details for the PostgreSQL provider/server."
    )
    database: list[DatabaseConfig] | dict[str, DatabaseConfig] = Field(
        default_factory=list,
        description="Databases to manage, provided as a list or a mapping keyed by database name.",
    )
    users: list[UserConfig] = Field(
        default_factory=list, description="PostgreSQL users/roles to create and manage."
    )
    custom_sql: list[CustomSQLQuery] = Field(
        default_factory=list,
        description="Custom SQL statements to render and execute after database/user/privilege setup.",
    )

    @model_validator(mode="after")
    def normalize_database(self) -> SQLConfig:
        if isinstance(self.database, list):
            self.database = {database.name: database for database in self.database}
        return self


# Ansible dynamically loads filter plugins, so Pydantic cannot rely on an
# importable module name to resolve postponed annotations.
SQLConfig.model_rebuild(_types_namespace=globals())


@contextmanager
def _temporary_environment(values: Mapping[str, Any]):
    old_values = {key: os.environ.get(key) for key in values}
    try:
        os.environ.update({key: str(value) for key, value in values.items()})
        yield
    finally:
        for key, value in old_values.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value


def _materialize(value: Any) -> Any:
    """Convert Ansible's lazy mappings and sequences to plain Python values."""
    if isinstance(value, Mapping):
        return {key: _materialize(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [_materialize(item) for item in value]
    return value


def _validate(
    config: Mapping[str, Any], environment: Mapping[str, Any] | None
) -> dict[str, Any]:
    if not isinstance(config, Mapping):
        raise ValueError("cloudutil_postgres_config must be a mapping")
    if environment is not None and not isinstance(environment, Mapping):
        raise ValueError("cloudutil_postgres_environment must be a mapping")

    with _temporary_environment(_materialize(environment or {})):
        return SQLConfig.model_validate(_materialize(config)).model_dump(mode="python")


def cloudutil_sql_config(
    config: Mapping[str, Any], environment: Mapping[str, Any] | None = None
) -> dict[str, Any]:
    """Validate an inline config; SQL Jinja syntax must be marked !unsafe."""
    return _validate(config, environment)


def cloudutil_sql_config_file(
    path: str | Path, environment: Mapping[str, Any] | None = None
) -> dict[str, Any]:
    """Load a schema YAML file without Ansible rendering its custom SQL Jinja."""
    config_path = Path(path).expanduser()
    if not config_path.is_file():
        raise ValueError(f"cloudutil_postgres_config_file is not a file: {config_path}")

    config = yaml.safe_load(config_path.read_text())
    if not isinstance(config, Mapping):
        raise ValueError("SQL configuration is empty or not a mapping")
    return _validate(config, environment)
