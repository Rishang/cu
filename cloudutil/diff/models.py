"""Pydantic models for cu diff configuration."""

from pydantic import BaseModel, model_validator


class Diff(BaseModel):
    files: list[str]

    # Ignore diff entries if ANY path segment contains these keys
    ignore_keys: list[str] = []

    # Ignore diff entries if values contain these patterns (case-insensitive substring)
    ignore_patterns: list[str] = []

    @model_validator(mode="after")
    def at_least_two_files(self) -> "Diff":
        if len(self.files) < 2:
            raise ValueError(
                f"'files' requires at least 2 entries, got {len(self.files)}"
            )
        return self


class DiffConfig(BaseModel):
    global_ignore_keys: list[str] = []
    global_ignore_patterns: list[str] = []
    diffs: list[Diff]

    @model_validator(mode="after")
    def at_least_one_diff(self) -> "DiffConfig":
        if not self.diffs:
            raise ValueError("'diffs' must contain at least one entry")
        return self
