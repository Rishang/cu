import os
from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any

from jinja2 import Environment, FileSystemLoader
from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

from cloudutil.utils import resolve_env_variable

SSL_MODES = {"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
SSL_VERIFY_MODES = {"verify-ca", "verify-full"}


class ProviderConfig(BaseModel):
    """Base provider configuration"""

    name: str = Field(
        description="Database provider type. Currently only 'postgres' is supported."
    )
    version: str | int = Field(
        description="PostgreSQL server version (e.g. 17 or '17.2')."
    )
    host: str = Field(description="Hostname or IP address of the database server.")
    port: int = Field(
        description="TCP port the database server listens on (default: 5432)."
    )
    username: str = Field(
        description="Login role used to connect. Supports ${ENV_VAR} substitution."
    )
    password: str = Field(
        description="Password for the login role. Supports ${ENV_VAR} substitution."
    )
    cert: str | None = Field(
        default=None,
        description="Path to SSL root certificate. Required when ssl_mode is 'verify-ca' or 'verify-full'.",
    )
    ssl_mode: str | None = Field(
        default=None,
        description=f"SSL connection mode. One of: {', '.join(sorted(SSL_MODES))}.",
    )

    @field_validator("username", "password", mode="before")
    @classmethod
    def resolve_env_vars(cls, v: str, info) -> str:
        return resolve_env_variable(v, f"provider.{info.field_name}")

    @model_validator(mode="after")
    def validate_ssl(self) -> "ProviderConfig":
        if self.ssl_mode and self.ssl_mode not in SSL_MODES:
            raise ValueError(
                f"provider.ssl_mode '{self.ssl_mode}' is not valid. "
                f"Choose from: {', '.join(sorted(SSL_MODES))}"
            )
        if self.cert and self.ssl_mode and self.ssl_mode not in SSL_VERIFY_MODES:
            raise ValueError(
                f"provider.cert requires ssl_mode 'verify-ca' or 'verify-full', got '{self.ssl_mode}'"
            )
        return self


class ExtensionConfig(BaseModel):
    """Database extension configuration"""

    name: str = Field(
        description="PostgreSQL extension name as it appears in CREATE EXTENSION (e.g. 'uuid-ossp', 'pgcrypto')."
    )


class DatabaseConfig(BaseModel):
    """Database configuration"""

    name: str = Field(description="Name of the database to manage.")
    create: bool = Field(
        default=True,
        description="Create the database if it does not exist. Set to false to manage an existing database.",
    )
    extensions: list[ExtensionConfig] = Field(
        default_factory=list,
        description="PostgreSQL extensions to install in this database.",
    )


class PrivilegeConfig(BaseModel):
    """User privilege configuration"""

    db: str = Field(description="Target database name this privilege applies to.")
    db_schema: str = Field(
        default="public",
        description="Schema within the database. Defaults to 'public'.",
    )
    readwrite: bool = Field(
        default=False,
        description="Grant SELECT, INSERT, UPDATE, DELETE on the specified tables. Mutually exclusive with readonly.",
    )
    readonly: bool = Field(
        default=False,
        description="Grant SELECT-only access on the specified tables. Mutually exclusive with readwrite.",
    )
    tables: list[str] = Field(
        default_factory=list,
        description="Tables to grant access to. Use ['ALL'] to target all current and future tables in the schema.",
    )

    @model_validator(mode="after")
    def validate_access_flags(self) -> "PrivilegeConfig":
        if self.readwrite and self.readonly:
            raise ValueError(
                f"privilege for db '{self.db}': readwrite and readonly cannot both be true"
            )
        return self


class UserConfig(BaseModel):
    """User configuration"""

    name: str = Field(description="PostgreSQL role/user name to create or manage.")
    password: str = Field(
        description="Password for the role. Supports ${ENV_VAR} substitution."
    )
    privileges: list[PrivilegeConfig] = Field(
        default_factory=list, description="Database privileges to grant to this user."
    )

    @field_validator("password", mode="before")
    @classmethod
    def resolve_password(cls, v: str) -> str:
        return resolve_env_variable(v, "user.password")


class CustomSQLQuery(BaseModel):
    """Arbitrary SQL executed after databases, extensions, users, and privileges."""

    query: str = Field(
        ...,
        description="Jinja template source string from YAML (not the rendered SQL).",
    )
    query_raw: str = Field(
        default="", description="Rendered SQL set in model_post_init."
    )
    template_context: dict[str, Any] = Field(
        default_factory=dict,
        description="Key/value pairs passed as Jinja2 template variables.",
    )
    loader_path: str | list[str] = Field(
        default=".",
        description="Root path(s) for FileSystemLoader used to resolve {% include %} and {% extends %} in templates.",
    )
    inject_env: bool = Field(
        default=True,
        description="When true, exposes os.environ as {{ env.VAR }} inside Jinja2 templates.",
    )
    database: str = Field(
        default="postgres",
        description="Database to connect to when executing this query.",
    )
    params: list[Any] = Field(
        default_factory=list,
        description="Positional parameters passed to the query (psycopg2 %s placeholders).",
    )
    name: str | None = Field(
        default=None,
        description="Optional label for this query, used in change reports and logs.",
    )

    @field_validator("query", "database", mode="before")
    @classmethod
    def _nonblank(cls, v: Any, info) -> str:
        if isinstance(v, str) and v.strip():
            return v.strip()
        raise ValueError(f"custom_sql.{info.field_name} must be a non-empty string")

    def model_post_init(self, __context: Any) -> None:
        """Render the Jinja template and store result in query_raw."""
        paths = (
            [self.loader_path]
            if isinstance(self.loader_path, str)
            else self.loader_path
        )
        resolved = []
        for p in paths:
            path = Path(p).expanduser().resolve()
            if not path.is_dir():
                raise ValueError(
                    f"custom_sql.loader_path must be an existing directory: {path}"
                )
            resolved.append(str(path))

        jinja_env = Environment(loader=FileSystemLoader(resolved), autoescape=False)
        if self.inject_env:
            jinja_env.globals["env"] = os.environ

        rendered = jinja_env.from_string(self.query).render(**self.template_context)
        object.__setattr__(self, "query_raw", rendered)


class SQLConfig(BaseModel):
    """Complete SQL configuration schema"""

    model_config = ConfigDict(
        json_schema_extra={
            "example": {
                "provider": {
                    "name": "postgres",
                    "version": 17,
                    "host": "localhost",
                    "port": 5432,
                    "username": "${POSTGRES_USER}",
                    "password": "${POSTGRES_PASSWORD}",
                    "ssl_mode": None,
                    "cert": None,
                },
                "database": [
                    {
                        "name": "myapp",
                        "create": True,
                        "extensions": [{"name": "uuid-ossp"}, {"name": "pgcrypto"}],
                    }
                ],
                "users": [
                    {
                        "name": "app_readwrite",
                        "password": "${APP_RW_PASSWORD}",
                        "privileges": [
                            {
                                "db": "myapp",
                                "db_schema": "public",
                                "readwrite": True,
                                "tables": ["ALL"],
                            }
                        ],
                    },
                    {
                        "name": "app_readonly",
                        "password": "${APP_RO_PASSWORD}",
                        "privileges": [
                            {
                                "db": "myapp",
                                "db_schema": "public",
                                "readonly": True,
                                "tables": ["users", "sessions"],
                            }
                        ],
                    },
                ],
                "custom_sql": [
                    {
                        "name": "seed",
                        "database": "myapp",
                        "query": "INSERT INTO settings (key, value) VALUES ('init', 'true') ON CONFLICT DO NOTHING",
                    }
                ],
            }
        }
    )

    provider: ProviderConfig = Field(description="Database server connection details.")
    database: list[DatabaseConfig] | dict[str, DatabaseConfig] = Field(
        default_factory=list,
        description="Databases to create or manage on the provider.",
    )
    users: list[UserConfig] = Field(
        default_factory=list,
        description="Roles/users to create and configure with their privileges.",
    )
    custom_sql: list[CustomSQLQuery] = Field(
        default_factory=list,
        description="Arbitrary SQL statements executed after databases, users, and privileges are applied.",
    )

    @model_validator(mode="after")
    def normalize_database(self) -> "SQLConfig":
        if isinstance(self.database, list):
            self.database = {db.name: db for db in self.database}
        return self


class BaseSQLProvider(ABC):
    """Abstract base class for SQL providers"""

    def __init__(self, config: SQLConfig):
        self.config = config

    @abstractmethod
    def connect(self) -> None: ...

    @abstractmethod
    def disconnect(self) -> None: ...

    @abstractmethod
    def create_database(self, db_config: DatabaseConfig) -> None: ...

    @abstractmethod
    def install_extensions(
        self, db_name: str, extensions: list[ExtensionConfig]
    ) -> None: ...

    @abstractmethod
    def create_user(self, user_config: UserConfig) -> None: ...

    @abstractmethod
    def grant_privileges(self, user_name: str, privilege: PrivilegeConfig) -> None: ...

    @abstractmethod
    def execute(self) -> None: ...

    def __enter__(self):
        self.connect()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.disconnect()
