"""Pydantic models for cu diff configuration."""

from typing import Annotated, Literal

from pydantic import BaseModel, BeforeValidator, ConfigDict, Field, model_validator


def _coerce_patterns(v: object) -> list[str]:
    """Accept 'a,b' or ['a', 'b'] or ['a,b', 'c'] — always returns a flat list."""
    if isinstance(v, str):
        return [tok.strip() for tok in v.split(",") if tok.strip()]
    if isinstance(v, list):
        return [
            tok.strip() for item in v for tok in str(item).split(",") if tok.strip()
        ]
    return v  # let pydantic handle other types


Patterns = Annotated[list[str], BeforeValidator(_coerce_patterns)]

Format = Literal["unified", "table", "json"]


class Diff(BaseModel):
    files: list[str] = Field(
        description="Two or more file paths to compare (JSON, YAML, TOML, or HCL). "
        "When 3+ files are given, all N-choose-2 pairs are compared. "
        "Paths are resolved relative to the config file location."
    )
    ignore_keys: list[str] = Field(
        default=[],
        description="Suppress diff entries whose dot-separated path contains any of these key segments "
        "(exact segment match at any depth). Merged with global_ignore_keys.",
    )
    ignore_patterns: Patterns = Field(
        default=[],
        description="Environment/marker tokens to strip before comparing values. "
        "A changed entry is suppressed when both values, after stripping these tokens, are identical. "
        "Accepts a list or a comma-separated string (e.g. 'qa,prod,stage'). "
        "Merged with global_ignore_patterns.",
    )
    query: str | None = Field(
        default=None,
        description="Filter diff entries for this pair only. "
        "Bare prefix: 'configmap.data' keeps only paths under that key. "
        "JMESPath expression: '[?kind==\"changed\"]' for arbitrary filtering. "
        "Overrides the top-level query for this pair.",
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
                "query": "configmap.data",
                "global_ignore_keys": ["metadata", "status"],
                "global_ignore_patterns": "qa,prod,stage",
                "diffs": [
                    {
                        "files": [
                            "helm/admin/values-qa.yaml",
                            "helm/admin/values-prod.yaml",
                        ],
                        "ignore_keys": ["timestamp"],
                        "query": "configmap.data",
                    },
                    {
                        "files": [
                            "helm/app/values-dev.yaml",
                            "helm/app/values-stage.yaml",
                            "helm/app/values-prod.yaml",
                        ],
                    },
                ],
            }
        }
    )

    format: Format = Field(
        default="table",
        description="Output format: 'table' (default, rich table), 'unified' (git-diff style), or 'json' (machine-readable). "
        "Overridden by --format / -o / --unified on the command line.",
    )
    query: str | None = Field(
        default=None,
        description="Global filter applied to every diff pair. "
        "Bare prefix: 'configmap.data' keeps only paths under that key. "
        "JMESPath expression: '[?kind==\"changed\"]' for arbitrary filtering. "
        "Overridden by -q on the command line; per-pair 'query' takes precedence.",
    )
    global_ignore_keys: list[str] = Field(
        default=[],
        description="Key segments to suppress across all diff pairs. "
        "A path is suppressed if any segment matches at any depth "
        "(e.g. 'metadata' suppresses 'spec.metadata.name').",
    )
    global_ignore_patterns: Patterns = Field(
        default=[],
        description="Environment/marker tokens stripped from values before comparing. "
        "A changed entry is suppressed when both values are identical after stripping. "
        "Accepts a list or a comma-separated string (e.g. 'qa,prod,stage'). "
        "Overridden by --ignore-pattern on the command line.",
    )
    diffs: list[Diff] = Field(
        description="List of diff entries. Each entry compares two or more files. "
        "When 3+ files are given, all N-choose-2 pairs are compared automatically.",
    )

    @model_validator(mode="after")
    def at_least_one_diff(self) -> "DiffConfig":
        if not self.diffs:
            raise ValueError("'diffs' must contain at least one entry")
        return self
