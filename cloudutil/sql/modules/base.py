import os
from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any

from jinja2 import Environment, FileSystemLoader
from pydantic import BaseModel, Field, field_validator, model_validator

from cloudutil.utils import resolve_env_variable

SSL_MODES = {"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
SSL_VERIFY_MODES = {"verify-ca", "verify-full"}


class ProviderConfig(BaseModel):
    """Base provider configuration"""

    name: str
    version: str | int
    host: str
    port: int
    username: str
    password: str
    cert: str | None = None
    ssl_mode: str | None = None

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

    name: str


class DatabaseConfig(BaseModel):
    """Database configuration"""

    name: str
    create: bool = True
    extensions: list[ExtensionConfig] = Field(default_factory=list)


class PrivilegeConfig(BaseModel):
    """User privilege configuration"""

    db: str
    db_schema: str = "public"
    readwrite: bool = False
    readonly: bool = False
    tables: list[str] = Field(default_factory=list)

    @model_validator(mode="after")
    def validate_access_flags(self) -> "PrivilegeConfig":
        if self.readwrite and self.readonly:
            raise ValueError(
                f"privilege for db '{self.db}': readwrite and readonly cannot both be true"
            )
        return self


class UserConfig(BaseModel):
    """User configuration"""

    name: str
    password: str
    privileges: list[PrivilegeConfig] = Field(default_factory=list)

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
    template_context: dict[str, Any] = Field(default_factory=dict)
    loader_path: str | list[str] = Field(
        default=".", description="Root path(s) for FileSystemLoader."
    )
    inject_env: bool = Field(
        default=True, description="Expose os.environ as {{ env.VAR }} in templates."
    )
    database: str = "postgres"
    params: list[Any] = Field(default_factory=list)
    name: str | None = None

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

    provider: ProviderConfig
    database: list[DatabaseConfig] | dict[str, DatabaseConfig] = Field(
        default_factory=list
    )
    users: list[UserConfig] = Field(default_factory=list)
    custom_sql: list[CustomSQLQuery] = Field(default_factory=list)

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
