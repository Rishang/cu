"""JMESPath-based YAML diff — moved from `cu os ydiff` to `cu diff --ydiff`."""

from pathlib import Path

import typer
from rich.rule import Rule

from cloudutil.utils import console
from cloudutil.os_utils.yaml_diff import (
    DiffCheckConfig,
    compare_pair,
    extract,
    load_yaml,
)


def run(config: Path) -> None:
    """Run the JMESPath YAML diff checker against *config* and exit."""
    if not config.exists():
        console.print(f"[bold red][ERROR][/bold red] Config file not found: {config}")
        raise typer.Exit(1)

    cfg = DiffCheckConfig.from_yaml(config)
    total_pairs = sum(len(e.pairs()) for e in cfg.checks)

    console.print(
        Rule(
            f"[bold]YAML DIFF[/bold]  [dim]·[/dim]  "
            f"[cyan]{len(cfg.checks)} check(s)[/cyan]  [dim]·[/dim]  "
            f"[cyan]{total_pairs} pair(s)[/cyan]",
            style="cyan",
        )
    )

    total_issues = 0

    for idx, entry in enumerate(cfg.checks, 1):
        loaded: dict[str, dict] = {fe.alias: load_yaml(fe.path) for fe in entry.files}

        for pair_idx, (fa, fb) in enumerate(entry.pairs(), 1):
            console.print(
                f"\n[dim][{idx}/{len(cfg.checks)}] pair {pair_idx}/{len(entry.pairs())}[/dim]  "
                f"[magenta bold]{fa.alias}[/magenta bold] [dim]↔[/dim] [blue bold]{fb.alias}[/blue bold]"
            )

            try:
                node_a = extract(loaded[fa.alias], entry.jsmec)
            except KeyError as exc:
                console.print(
                    f"  [yellow][WARN] {exc} in '{fa.alias}' — skipping pair.[/yellow]"
                )
                continue

            try:
                node_b = extract(loaded[fb.alias], entry.jsmec)
            except KeyError as exc:
                console.print(
                    f"  [yellow][WARN] {exc} in '{fb.alias}' — skipping pair.[/yellow]"
                )
                continue

            total_issues += compare_pair(
                node_a, node_b, fa, fb, entry.jsmec, entry.ignore_patterns
            )

    console.print()
    if total_issues == 0:
        console.print(
            Rule(
                "[bold green]🎉  ALL CHECKS PASSED — no differences detected.[/bold green]",
                style="green",
            )
        )
    else:
        console.print(
            Rule(
                f"[bold red]❌  {total_issues} total issue(s) across all pairs.[/bold red]",
                style="red",
            )
        )
    console.print()

    raise typer.Exit(1 if total_issues else 0)
