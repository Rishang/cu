"""CLI entry point for `cu diff` — semantic diff of structured config files."""

from enum import Enum
from pathlib import Path
from typing import Annotated

import typer
import yaml
from rich.rule import Rule

from cloudutil.utils import console
from .engine import compute_diff
from .filters import apply_filters
from .loader import get_git_branch, load_file
from .models import Diff, DiffConfig
from .renderer import render
from . import ydiff as _ydiff


class Format(str, Enum):
    unified = "unified"
    table = "table"
    json = "json"


class SchemaTarget(str, Enum):
    diff = "diff"
    ydiff = "ydiff"


def _err(msg: str) -> None:
    console.print(f"[bold red][ERROR][/bold red] {msg}")


def diff_cmd(
    config: Annotated[
        Path | None,
        typer.Option(
            "--config", "-c", help="Diff config YAML file.", show_default=False
        ),
    ] = None,
    files: Annotated[
        list[Path] | None,
        typer.Option(
            "-f",
            "--file",
            help="File to compare (repeat twice: -f a.yaml -f b.yaml).",
            show_default=False,
        ),
    ] = None,
    ignore_key: Annotated[
        list[str] | None,
        typer.Option(
            "--ignore-key",
            help="Suppress paths containing this key segment (repeatable).",
            show_default=False,
        ),
    ] = None,
    ignore_pattern: Annotated[
        list[str] | None,
        typer.Option(
            "--ignore-pattern",
            help="Suppress diffs where a value contains this substring (repeatable).",
            show_default=False,
        ),
    ] = None,
    format: Annotated[
        Format,
        typer.Option("--format", "-o", help="Output format.", show_default=True),
    ] = Format.unified,
    color: Annotated[
        bool,
        typer.Option("--color/--no-color", help="Enable/disable colored output."),
    ] = True,
    ydiff: Annotated[
        Path | None,
        typer.Option(
            "--ydiff",
            help="JMESPath-based YAML diff config file (replaces `cu os ydiff`).",
            show_default=False,
        ),
    ] = None,
    print_schema: Annotated[
        SchemaTarget | None,
        typer.Option(
            "--print-schema",
            help="Print config JSON schema and exit. Choices: diff (--config format), ydiff (--ydiff format).",
            show_default=False,
        ),
    ] = None,
) -> None:
    """
    [bold cyan]Semantic diff[/bold cyan] — compare JSON, YAML, or TOML config files structurally.

    \b
    Compare two files inline:
      cu diff -f prod.yaml -f stage.yaml

    \b
    Compare using a config file (supports global ignore rules and multiple pairs):
      cu diff --config diff_config.yaml

    \b
    JMESPath-based YAML diff (formerly `cu os ydiff`):
      cu diff --ydiff ydiff_config.yaml

    \b
    Common flags:
      --ignore-key metadata      suppress paths containing 'metadata'
      --ignore-pattern dev       suppress values containing 'dev'
      --format table             table layout
      --format json              machine-readable JSON
    """
    if print_schema is not None:
        import yaml as _yaml

        if print_schema == SchemaTarget.diff:
            schema = DiffConfig.model_json_schema()
        else:
            from cloudutil.os_utils.yaml_diff import DiffCheckConfig

            schema = DiffCheckConfig.model_json_schema()

        print(_yaml.dump(schema, default_flow_style=False, sort_keys=False))
        raise typer.Exit(0)

    if ydiff:
        _ydiff.run(ydiff)
        return

    if files and config:
        _err("Use either -f flags or a config file argument, not both.")
        raise typer.Exit(1)

    if not files and not config:
        _err(
            "Specify -f <file> -f <file>, --config <config.yaml>, or --ydiff <ydiff_config.yaml>."
        )
        raise typer.Exit(1)

    if files:
        _run_inline(files, ignore_key or [], ignore_pattern or [], format, color)
    else:
        _run_config(config, format, color)  # type: ignore[arg-type]


def _run_inline(
    files: list[Path],
    ignore_keys: list[str],
    ignore_patterns: list[str],
    format: Format,
    color: bool,
) -> None:
    if len(files) != 2:
        _err(f"Exactly 2 -f/--file values required, got {len(files)}.")
        raise typer.Exit(1)

    entry = Diff(
        files=[str(f) for f in files],
        ignore_keys=ignore_keys,
        ignore_patterns=ignore_patterns,
    )
    cfg = DiffConfig(diffs=[entry])
    raise typer.Exit(1 if _execute_diff(entry, cfg, format, color) else 0)


def _run_config(config: Path, format: Format, color: bool) -> None:
    if not config.exists():
        _err(f"Config file not found: {config}")
        raise typer.Exit(1)

    try:
        raw = yaml.safe_load(config.read_text(encoding="utf-8")) or {}
        cfg = DiffConfig(**raw)
    except Exception as exc:
        _err(str(exc))
        raise typer.Exit(1)

    total, n = 0, len(cfg.diffs)
    for i, entry in enumerate(cfg.diffs, 1):
        a, b = Path(entry.files[0]).name, Path(entry.files[1]).name
        console.print(
            Rule(
                f"[bold]DIFF {i}/{n}[/bold]  [dim]·[/dim]  [cyan]{a}[/cyan]  [dim]↔[/dim]  [cyan]{b}[/cyan]",
                style="cyan",
            )
        )
        total += _execute_diff(entry, cfg, format, color)
        console.print()

    if n > 1:
        if total == 0:
            console.print(
                Rule(
                    "[bold green]✅  ALL DIFFS PASSED — no differences detected.[/bold green]",
                    style="green",
                )
            )
        else:
            console.print(
                Rule(
                    f"[bold red]❌  {total} difference(s) across {n} pair(s).[/bold red]",
                    style="red",
                )
            )

    raise typer.Exit(1 if total else 0)


def _execute_diff(entry: Diff, cfg: DiffConfig, format: Format, color: bool) -> int:
    """Load, diff, filter, and render one pair. Returns the number of differences."""
    file_a, file_b = entry.files[0], entry.files[1]

    try:
        data_a = load_file(file_a)
        data_b = load_file(file_b)
    except (FileNotFoundError, ValueError) as exc:
        _err(str(exc))
        raise typer.Exit(1)

    filtered = apply_filters(
        compute_diff(data_a, data_b),
        global_ignore_keys=cfg.global_ignore_keys,
        local_ignore_keys=entry.ignore_keys,
        global_ignore_patterns=cfg.global_ignore_patterns,
        local_ignore_patterns=entry.ignore_patterns,
    )

    render(
        filtered,
        format=format,
        file_a=Path(file_a).name,
        file_b=Path(file_b).name,
        branch_a=get_git_branch(file_a),
        branch_b=get_git_branch(file_b),
        color=color,
    )
    return len(filtered)
