#!/usr/bin/env python3
"""YAML Diff Checker using JMESPath + Rich."""

import re
import subprocess
from difflib import SequenceMatcher
from itertools import combinations
from pathlib import Path
from typing import Any

import jmespath
import typer
import yaml
from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator
from rich import box
from rich.panel import Panel
from rich.table import Table
from rich.text import Text

from cloudutil.utils import console

# ── Types ──────────────────────────────────────────────────────────────────────

type FlatDict = dict[str, Any]
type CompiledPatterns = list[re.Pattern]

# ── Git helpers ────────────────────────────────────────────────────────────────


def get_git_branch(file_path: str) -> str:
    """Return the current git branch for the repo containing *file_path*."""
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--abbrev-ref", "HEAD"],
            cwd=Path(file_path).resolve().parent,
            capture_output=True,
            text=True,
            check=True,
        )
        if (branch := result.stdout.strip()) and branch != "HEAD":
            return branch
        raise ValueError("detached HEAD — no branch name available")
    except subprocess.CalledProcessError as e:
        raise ValueError(
            f"Could not determine git branch for '{file_path}': {e.stderr.strip()}"
        ) from e


# ── Pydantic models ────────────────────────────────────────────────────────────


class FileEntry(BaseModel):
    alias: str = Field(
        description="Display name for this file in diff output. Use '$branch' to resolve to the current git branch name automatically."
    )
    path: str = Field(
        description="Path to the YAML file. Relative paths are resolved from the working directory."
    )

    @field_validator("alias", "path")
    @classmethod
    def not_empty(cls, v: str) -> str:
        if not v.strip():
            raise ValueError("must not be empty")
        return v

    @classmethod
    def from_dict(cls, data: dict) -> "FileEntry":
        """Parse a single-key ``{alias: path}`` mapping. ``$branch`` is resolved to the git branch."""
        if len(data) != 1:
            raise ValueError(
                f"Each 'files' entry must be a single-key mapping, got: {data}"
            )
        alias, path = next(iter(data.items()))
        alias, path = str(alias), str(path)
        if alias == "$branch":
            alias = get_git_branch(path)
        return cls(alias=alias, path=path)


class DiffCheckEntry(BaseModel):
    jsmec: str = Field(
        description="JMESPath expression used to extract the node to compare from each YAML file (e.g. 'configMap', 'spec.template')."
    )
    files: list[FileEntry] = Field(
        default_factory=list,
        description="List of YAML files to compare. At least 2 required. Every combination of pairs is compared.",
    )
    ignore_patterns: list[str] = Field(
        default_factory=list,
        description="Suppress diff lines where the value contains any of these substrings (case-insensitive).",
    )

    @field_validator("jsmec")
    @classmethod
    def jsmec_not_empty(cls, v: str) -> str:
        if not v.strip():
            raise ValueError("'jsmec' must not be empty")
        return v

    @model_validator(mode="after")
    def at_least_two_files(self) -> "DiffCheckEntry":
        if len(self.files) < 2:
            raise ValueError(
                f"'files' requires at least 2 entries, got {len(self.files)}"
            )
        return self

    @classmethod
    def from_dict(cls, data: dict) -> "DiffCheckEntry":
        if missing := [k for k in ("jsmec", "files") if k not in data]:
            raise KeyError(f"Missing required key(s): {missing}")
        if not isinstance(data["files"], list):
            raise ValueError("'files' must be a list")
        if not isinstance(data.get("ignore_patterns", []), list):
            raise ValueError("'ignore_patterns' must be a list")
        return cls(
            jsmec=str(data["jsmec"]),
            files=[FileEntry.from_dict(f) for f in data["files"]],
            ignore_patterns=[str(p) for p in data.get("ignore_patterns", [])],
        )

    def pairs(self) -> list[tuple[FileEntry, FileEntry]]:
        return list(combinations(self.files, 2))


class DiffCheckConfig(BaseModel):
    """Configuration file format for `cu diff --ydiff`. Top-level key is 'ydiff'."""

    model_config = ConfigDict(
        json_schema_extra={
            "example": {
                "ydiff": [
                    {
                        "jsmec": "configMap",
                        "files": [
                            {"app-v1": "./values/main.yaml"},
                            {"$branch": "./values/feature.yaml"},
                        ],
                        "ignore_patterns": ["test", "dev"],
                    }
                ]
            }
        }
    )

    checks: list[DiffCheckEntry] = Field(
        default_factory=list,
        description="List of JMESPath diff checks. Each entry compares a specific node across multiple YAML files.",
    )

    @model_validator(mode="after")
    def at_least_one_check(self) -> "DiffCheckConfig":
        if not self.checks:
            raise ValueError("Config must contain at least one entry under 'ydiff'")
        return self

    @classmethod
    def from_yaml(cls, path: Path) -> "DiffCheckConfig":
        if not path.exists():
            console.print(
                f"[bold red]  [ERROR] Config file not found: {path}[/bold red]"
            )
            raise typer.Exit(1)

        raw = yaml.safe_load(path.read_text()) or {}
        raw_checks = raw.get("ydiff", [])

        if not isinstance(raw_checks, list):
            console.print("[bold red]  [ERROR] 'ydiff' must be a list.[/bold red]")
            raise typer.Exit(1)

        entries: list[DiffCheckEntry] = []
        for i, item in enumerate(raw_checks):
            try:
                entries.append(DiffCheckEntry.from_dict(item))
            except (KeyError, ValueError) as e:
                console.print(f"[bold red]  [ERROR] diffcheck[{i}]: {e}[/bold red]")
                raise typer.Exit(1)

        try:
            return cls(checks=entries)
        except ValueError as e:
            console.print(f"[bold red]  [ERROR] {e}[/bold red]")
            raise typer.Exit(1)


# ── Core helpers ───────────────────────────────────────────────────────────────


def load_yaml(path: str) -> dict:
    p = Path(path)
    if not p.exists():
        console.print(f"[bold red]  [ERROR] File not found: {path}[/bold red]")
        raise typer.Exit(1)
    return yaml.safe_load(p.read_text()) or {}


def extract(data: dict, expr: str) -> Any:
    if (result := jmespath.search(expr, data)) is None:
        raise KeyError(f"JMESPath '{expr}' returned nothing")
    return result


def flatten(obj: Any, prefix: str = "") -> FlatDict:
    if isinstance(obj, dict):
        return {
            k: v
            for key, val in obj.items()
            for k, v in flatten(val, f"{prefix}.{key}" if prefix else key).items()
        }
    if isinstance(obj, list):
        return {
            k: v
            for i, val in enumerate(obj)
            for k, v in flatten(val, f"{prefix}[{i}]").items()
        }
    return {prefix: obj}


# ── Pattern matching ───────────────────────────────────────────────────────────


def _compile(patterns: list[str]) -> CompiledPatterns:
    return [
        re.compile(r"(?<![A-Za-z0-9])" + re.escape(p) + r"(?![A-Za-z0-9])")
        for p in patterns
    ]


def _any_match(compiled: CompiledPatterns, *values: Any) -> bool:
    return any(pat.search(str(v)) for pat in compiled for v in values)


def _strip_all(compiled: CompiledPatterns, value: str) -> str:
    for pat in compiled:
        value = pat.sub("", value)
    return value


def _ignore_diff(
    compiled: CompiledPatterns,
    key: str,
    v1: Any,
    v2: Any,
    similarity_threshold: float = 0.9,
) -> bool:
    """
    Ignore a value difference when the key matches a pattern, OR both values
    match a pattern and are highly similar after stripping pattern tokens (ratio ≥ 0.9).
    """
    if _any_match(compiled, key):
        return True
    if _any_match(compiled, v1) and _any_match(compiled, v2):
        s1, s2 = _strip_all(compiled, str(v1)), _strip_all(compiled, str(v2))
        return SequenceMatcher(None, s1, s2).ratio() >= similarity_threshold
    return False


# ── Rich output helpers ────────────────────────────────────────────────────────


def _simple_table(*rows: tuple, style: str = "red") -> Table:
    t = Table(box=box.SIMPLE, show_header=False, padding=(0, 2))
    t.add_column(style=style)
    t.add_column(style=style)
    for row in rows:
        t.add_row(*row)
    return t


# ── Comparison ─────────────────────────────────────────────────────────────────


def compare_pair(
    node1: Any,
    node2: Any,
    fa: FileEntry,
    fb: FileEntry,
    jsmec: str,
    ignore_patterns: list[str] | None = None,
) -> int:
    flat1, flat2 = (
        dict(sorted(flatten(node1).items())),
        dict(sorted(flatten(node2).items())),
    )
    all_keys = sorted(flat1.keys() | flat2.keys())

    compiled = _compile(ignore_patterns or [])
    missing_b, missing_a, value_diff, matching, ignored = [], [], [], [], []

    for key in all_keys:
        in1, in2 = key in flat1, key in flat2
        match (in1, in2):
            case (True, True) if flat1[key] == flat2[key]:
                matching.append(key)
            case (True, True) if compiled and _ignore_diff(
                compiled, key, flat1[key], flat2[key]
            ):
                ignored.append(key)
            case (True, True):
                value_diff.append((key, flat1[key], flat2[key]))
            case (True, False) if compiled and _any_match(compiled, key):
                ignored.append(key)
            case (True, False):
                missing_b.append((key, flat1[key]))
            case (False, True) if compiled and _any_match(compiled, key):
                ignored.append(key)
            case _:
                missing_a.append((key, flat2[key]))

    total_issues = len(missing_a) + len(missing_b) + len(value_diff)

    # ── Panel header ───────────────────────────────────────────────────────────
    header = Text()
    header.append("JMESPath  ", style="bold cyan")
    header.append(f"{jsmec}\n", style="bold white")
    header.append(f"{fa.alias.upper():<10} ", style="bold magenta")
    header.append(f"{fa.path}\n", style="dim")
    header.append(f"{fb.alias.upper():<10}  ", style="bold blue")
    header.append(f"{fb.path}", style="dim")
    if ignore_patterns:
        header.append("\nIgnoring  ", style="bold cyan")
        header.append(", ".join(ignore_patterns), style="dim italic")

    console.print(
        Panel(header, border_style="red" if total_issues else "green", expand=True)
    )

    # ── Missing in B ───────────────────────────────────────────────────────────
    if missing_b:
        console.print(
            f"  [bold red]✗  In [magenta]{fa.alias}[/magenta]"
            f" only — missing in [blue]{fb.alias}[/blue] ({len(missing_b)})[/bold red]"
        )
        console.print(_simple_table(*((f"- {k}", repr(v)) for k, v in missing_b)))

    # ── Missing in A ───────────────────────────────────────────────────────────
    if missing_a:
        console.print(
            f"  [bold red]✗  In [blue]{fb.alias}[/blue]"
            f" only — missing in [magenta]{fa.alias}[/magenta] ({len(missing_a)})[/bold red]"
        )
        console.print(_simple_table(*((f"+ {k}", repr(v)) for k, v in missing_a)))

    # ── Value differences ──────────────────────────────────────────────────────
    if value_diff:
        console.print(
            f"  [bold yellow]≠  Value differences ({len(value_diff)})[/bold yellow]"
        )
        t = Table(
            box=box.ROUNDED,
            show_header=True,
            header_style="bold yellow",
            padding=(0, 2),
        )
        t.add_column("Key", style="yellow", no_wrap=True)
        t.add_column(f"[magenta]{fa.alias}[/magenta]", style="red")
        t.add_column(f"[blue]{fb.alias}[/blue]", style="green")
        for key, v1, v2 in value_diff:
            t.add_row(key, repr(v1), repr(v2))
        console.print(t)

    # ── Matching ───────────────────────────────────────────────────────────────
    if matching:
        console.print(f"  [bold green]✓  Matching keys ({len(matching)})[/bold green]")
        console.print(
            _simple_table(
                *((f"= {k}", repr(flat1[k])) for k in matching), style="green"
            )
        )

    # ── Ignored ────────────────────────────────────────────────────────────────
    if ignored:
        console.print(
            f"  [bold dim]⊘  Ignored keys ({len(ignored)}) — matched ignore_patterns[/bold dim]"
        )
        t = Table(box=box.SIMPLE, show_header=False, padding=(0, 2))
        t.add_column(style="dim")
        for key in ignored:
            t.add_row(f"~ {key}")
        console.print(t)

    # ── Summary ────────────────────────────────────────────────────────────────
    if total_issues == 0:
        console.print(
            f"  [bold green]✅  No differences — "
            f"[magenta]{fa.alias}[/magenta] and [blue]{fb.alias}[/blue] "
            f"are identical at this path.[/bold green]"
        )
    else:
        console.print(
            f"  [bold yellow]⚠   {total_issues} issue(s): "
            f"{len(missing_a) + len(missing_b)} missing key(s), "
            f"{len(value_diff)} value difference(s).[/bold yellow]"
        )

    return total_issues
