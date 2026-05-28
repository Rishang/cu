"""CLI entry point for `cu diff` — semantic diff of structured config files."""

from enum import Enum
from pathlib import Path
from typing import Annotated

import typer
import yaml
from rich.rule import Rule

from cloudutil.utils import console
from .engine import compute_diff
from .filters import apply_filters, apply_query
from .loader import HCL_EXTENSIONS, get_git_branch, load_file
from .models import Diff, DiffConfig
from .renderer import render
from . import ydiff as _ydiff


class Format(str, Enum):
    unified = "unified"
    table = "table"
    json = "json"


_FORMAT_DEFAULT = Format.table  # fallback when neither CLI nor config specifies format


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
        Format | None,
        typer.Option("--format", "-o", help="Output format.", show_default=False),
    ] = None,
    unified: Annotated[
        bool,
        typer.Option(
            "--unified",
            "-u",
            help="Shorthand for --format unified (git-diff style).",
            is_flag=True,
        ),
    ] = False,
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
    query: Annotated[
        str | None,
        typer.Option(
            "-q",
            "--query",
            help=(
                "JMESPath query to filter diff entries. "
                "Each entry exposes: path, kind, old, new. "
                "Example: -q \"[?kind=='changed']\""
            ),
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
      --unified / -u             git-diff style output instead of table
      --ignore-key metadata      suppress paths containing 'metadata'
      --ignore-pattern dev       suppress values containing 'dev'
      --format json              machine-readable JSON
      -q spec.replicas           show only diffs under a path prefix
      -q "[?kind=='changed']"    JMESPath filter on diff entries
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

    # When no inputs are specified, auto-detect a default config file in cwd:
    # cu_diff.yml -> normal diff config; cu_ydiff.yml -> --ydiff config.
    # Prefer cu_diff.yml when both exist (one command exits the process, so only one runs).
    if not files and not config and not ydiff:
        default_diff = Path("cu_diff.yml")
        default_ydiff = Path("cu_ydiff.yml")
        if default_diff.exists():
            config = default_diff
        elif default_ydiff.exists():
            ydiff = default_ydiff
        else:
            _err(
                "Specify -f <file> -f <file>, --config <config.yaml>, or --ydiff <ydiff_config.yaml>. "
                "Or create cu_diff.yml / cu_ydiff.yml in the current directory."
            )
            raise typer.Exit(1)

    if ydiff:
        _ydiff.run(ydiff)
        return

    if files and config:
        _err("Use either -f flags or a config file argument, not both.")
        raise typer.Exit(1)

    # --unified beats --format; fall back to config.format or table default
    cli_format = Format.unified if unified else format
    if files:
        _run_inline(
            files,
            ignore_key or [],
            ignore_pattern or [],
            cli_format or _FORMAT_DEFAULT,
            color,
            query,
        )
    else:
        _run_config(config, cli_format, color, query)  # type: ignore[arg-type]


def _run_inline(
    files: list[Path],
    ignore_keys: list[str],
    ignore_patterns: list[str],
    format: Format,
    color: bool,
    query: str | None,
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
    raise typer.Exit(1 if _execute_diff(entry, cfg, format, color, query) else 0)


def _run_config(
    config: Path, cli_format: Format | None, color: bool, cli_query: str | None
) -> None:
    if not config.exists():
        _err(f"Config file not found: {config}")
        raise typer.Exit(1)

    try:
        raw = yaml.safe_load(config.read_text(encoding="utf-8")) or {}
        cfg = DiffConfig(**raw)
    except Exception as exc:
        _err(str(exc))
        raise typer.Exit(1)

    # CLI flags override config; config overrides built-in default
    effective_format = Format(cli_format or cfg.format)

    total, n = 0, len(cfg.diffs)
    for i, entry in enumerate(cfg.diffs, 1):
        # Query precedence: CLI -q > per-pair config query > global config query
        effective_query = cli_query or entry.query or cfg.query
        a, b = Path(entry.files[0]).name, Path(entry.files[1]).name
        console.print(
            Rule(
                f"[bold]DIFF {i}/{n}[/bold]  [dim]·[/dim]  [cyan]{a}[/cyan]  [dim]↔[/dim]  [cyan]{b}[/cyan]",
                style="cyan",
            )
        )
        total += _execute_diff(entry, cfg, effective_format, color, effective_query)
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


def _execute_diff(
    entry: Diff, cfg: DiffConfig, format: Format, color: bool, query: str | None
) -> int:
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

    if query:
        try:
            filtered = apply_query(filtered, query)
        except ValueError as exc:
            _err(str(exc))
            raise typer.Exit(1)

    hcl_pair = (
        Path(file_a).suffix.lower() in HCL_EXTENSIONS
        and Path(file_b).suffix.lower() in HCL_EXTENSIONS
    )
    render(
        filtered,
        format=format,
        file_a=Path(file_a).name,
        file_b=Path(file_b).name,
        branch_a=get_git_branch(file_a),
        branch_b=get_git_branch(file_b),
        color=color,
        value_style="hcl" if hcl_pair else "default",
    )
    return len(filtered)
