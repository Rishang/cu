"""Pydantic models for cu diff configuration."""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

Format = Literal["unified", "table", "json"]


class Diff(BaseModel):
    files: list[str] = Field(
        description="Exactly two file paths to compare (JSON, YAML, TOML, or HCL). "
        "Paths may be relative to the config file location."
    )
    ignore_keys: list[str] = Field(
        default=[],
        description="Suppress diff entries whose dot-separated path contains any of these key segments (exact match, any depth). "
        "Merged with global_ignore_keys.",
    )
    ignore_patterns: list[str] = Field(
        default=[],
        description="Suppress diff entries where the old or new value contains any of these substrings (case-insensitive). "
        "Merged with global_ignore_patterns.",
    )
    query: str | None = Field(
        default=None,
        description="JMESPath query or path prefix to filter diff entries for this pair. "
        "Overrides the top-level query for this pair only. "
        "Examples: 'spec.replicas', '[?kind==\"changed\"]'",
    )

    @model_validator(mode="after")
    def at_least_two_files(self) -> "Diff":
        if len(self.files) < 2:
            raise ValueError(
                f"'files' requires at least 2 entries, got {len(self.files)}"
            )
        return self


class DiffConfig(BaseModel):
    """Configuration file format for `cu diff --config`."""

    model_config = ConfigDict(
        json_schema_extra={
            "example": {
                "format": "table",
                "global_ignore_keys": ["metadata", "status"],
                "global_ignore_patterns": ["dev", "TEST"],
                "diffs": [
                    {
                        "files": ["config/prod.yaml", "config/stage.yaml"],
                        "ignore_keys": ["timestamp"],
                        "query": "spec",
                    },
                    {
                        "files": ["service-a.toml", "service-b.toml"],
                    },
                ],
            }
        }
    )

    format: Format = Field(
        default="table",
        description="Output format for all pairs: unified (git-diff style), table, or json. "
        "Overridden by --format / --table on the command line.",
    )
    query: str | None = Field(
        default=None,
        description="JMESPath query or path prefix applied to every pair. "
        "Overridden by -q on the command line. "
        "Per-pair 'query' takes precedence over this field.",
    )
    global_ignore_keys: list[str] = Field(
        default=[],
        description="Key segments to suppress across all diff pairs. "
        "A path is suppressed if any segment matches (e.g. 'metadata' suppresses 'spec.metadata.name').",
    )
    global_ignore_patterns: list[str] = Field(
        default=[],
        description="Value substrings to suppress across all diff pairs (case-insensitive). "
        "A diff entry is suppressed if the old or new value contains any of these strings.",
    )
    diffs: list[Diff] = Field(
        description="List of file pairs to compare. Each entry specifies the two files and optional per-pair ignore rules."
    )

    @model_validator(mode="after")
    def at_least_one_diff(self) -> "DiffConfig":
        if not self.diffs:
            raise ValueError("'diffs' must contain at least one entry")
        return self
