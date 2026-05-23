"""Render diff entries in unified (git-diff-style), table, or JSON format."""

import json as _json
from collections import defaultdict
from typing import Any, Literal

from rich import box
from rich.table import Table

from cloudutil.utils import console
from .engine import DiffEntry

OutputFormat = Literal["unified", "table", "json"]

# symbol, color  — used by both table and unified renderers
_KIND: dict[str, tuple[str, str]] = {
    "added": ("+", "green"),
    "removed": ("-", "red"),
    "changed": ("~", "yellow"),
    "type_changed": ("~", "yellow"),
}


def render(
    entries: list[DiffEntry],
    format: OutputFormat = "unified",
    file_a: str = "file_a",
    file_b: str = "file_b",
    branch_a: str | None = None,
    branch_b: str | None = None,
    color: bool = True,
) -> None:
    match format:
        case "table":
            _render_table(entries, file_a, file_b)
        case "json":
            _render_json(entries, file_a, file_b, branch_a, branch_b)
        case _:
            _render_unified(entries, file_a, file_b, branch_a, branch_b, color)


# ── Unified ───────────────────────────────────────────────────────────────────


def _render_unified(
    entries: list[DiffEntry],
    file_a: str,
    file_b: str,
    branch_a: str | None,
    branch_b: str | None,
    color: bool,
) -> None:
    def branch(name: str | None) -> str:
        return f"  [dim]({name})[/dim]" if name else ""

    console.print(f"[bold red]--- a/{file_a}[/bold red]{branch(branch_a)}")
    console.print(f"[bold green]+++ b/{file_b}[/bold green]{branch(branch_b)}")

    if not entries:
        console.print()
        console.print("[bold green]✓  No differences[/bold green]")
        return

    groups: dict[str, list[DiffEntry]] = defaultdict(list)
    for entry in sorted(entries, key=lambda e: e.path_str):
        groups[str(entry.path[0]) if entry.path else "(root)"].append(entry)

    all_lines: list[tuple[str, str, str]] = []
    sym_style = {"+": "green", "-": "red", "~": "yellow"}

    for section, group in sorted(groups.items()):
        n = len(group)
        label = f"({n} change{'s' if n > 1 else ''})"
        console.print()
        console.print(
            f"[bold cyan]@@ {section} @@[/bold cyan]  [dim]{label}[/dim]"
            if color
            else f"@@ {section} @@  {label}"
        )

        lines = _build_lines(group, section)
        pad = max((len(k) for _, k, _ in lines if k), default=0)

        for sym, key, val in lines:
            style = sym_style[sym]
            col = f"{key}:".ljust(pad + 1) if key else ""
            text = f"{sym}  {col}  {val}" if col else f"{sym}  {val}"
            console.print(f"[{style}]{text}[/{style}]" if color else text)

        all_lines.extend(lines)

    added = sum(1 for s, _, _ in all_lines if s == "+")
    removed = sum(1 for s, _, _ in all_lines if s == "-")
    changed = sum(1 for s, _, _ in all_lines if s == "~")

    summary = Table(
        box=box.SIMPLE_HEAD,
        show_header=True,
        header_style="dim",
        padding=(0, 1),
        expand=False,
        show_edge=False,
    )
    summary.add_column("+  added", style="green", justify="right")
    summary.add_column("-  removed", style="red", justify="right")
    summary.add_column("~  changed", style="yellow", justify="right")
    summary.add_row(str(added), str(removed), str(changed))
    console.print()
    console.print(summary)


def _build_lines(group: list[DiffEntry], section: str) -> list[tuple[str, str, str]]:
    """Convert a group of DiffEntry objects into (symbol, key, value) display tuples."""
    lines: list[tuple[str, str, str]] = []
    for entry in group:
        rel = _rel_key(entry.path_str, section)
        match entry.kind:
            case "added":
                lines.extend(
                    ("+", k, _fmt(v)) for k, v in _expand(rel, entry.new_value)
                )
            case "removed":
                lines.extend(
                    ("-", k, _fmt(v)) for k, v in _expand(rel, entry.old_value)
                )
            case "changed":
                old_v, new_v = entry.old_value, entry.new_value
                if _is_scalar(old_v) and _is_scalar(new_v):
                    lines.append(("~", rel, f"{_fmt(old_v)} → {_fmt(new_v)}"))
                else:
                    lines.append(("-", rel, _fmt(old_v)))
                    lines.append(("+", rel, _fmt(new_v)))
            case "type_changed":
                old_v, new_v = entry.old_value, entry.new_value
                note = f"  ({type(old_v).__name__} → {type(new_v).__name__})"
                if _is_scalar(old_v) and _is_scalar(new_v):
                    lines.append(("~", rel, f"{_fmt(old_v)} → {_fmt(new_v)}{note}"))
                else:
                    lines.append(("-", rel, _fmt(old_v) + note))
                    lines.append(("+", rel, _fmt(new_v)))
    return lines


def _rel_key(path_str: str, section: str) -> str:
    """Strip the top-level section prefix from a dotted path."""
    if path_str == section:
        return ""
    if path_str.startswith(section + "."):
        return path_str[len(section) + 1 :]
    if path_str.startswith(section + "["):
        return path_str[len(section) :]
    return path_str


def _expand(prefix: str, value: Any) -> list[tuple[str, Any]]:
    """Flatten an added/removed value to (key, leaf) pairs."""
    if isinstance(value, dict):
        pairs = [
            pair
            for k in sorted(value)
            for pair in _expand(f"{prefix}.{k}" if prefix else k, value[k])
        ]
        return pairs or [(prefix, value)]
    if isinstance(value, list):
        pairs = [
            pair for i, v in enumerate(value) for pair in _expand(f"{prefix}[{i}]", v)
        ]
        return pairs or [(prefix, value)]
    return [(prefix, value)]


def _is_scalar(v: Any) -> bool:
    return not isinstance(v, (dict, list))


# ── Table ─────────────────────────────────────────────────────────────────────


def _render_table(entries: list[DiffEntry], file_a: str, file_b: str) -> None:
    if not entries:
        console.print("[bold green]✓  No differences[/bold green]")
        return

    t = Table(
        box=box.ROUNDED, show_header=True, header_style="bold cyan", padding=(0, 2)
    )
    t.add_column("", style="bold", width=3, no_wrap=True)
    t.add_column("Path", style="cyan", no_wrap=True)
    t.add_column(f"{file_a} (old)", style="red")
    t.add_column(f"{file_b} (new)", style="green")

    for entry in sorted(entries, key=lambda e: e.path_str):
        sym, style = _KIND[entry.kind]
        old = "[dim]—[/dim]" if entry.kind == "added" else _fmt(entry.old_value)
        new = "[dim]—[/dim]" if entry.kind == "removed" else _fmt(entry.new_value)
        t.add_row(f"[{style}]{sym}[/{style}]", entry.path_str, old, new)

    console.print(t)


# ── JSON ──────────────────────────────────────────────────────────────────────


def _render_json(
    entries: list[DiffEntry],
    file_a: str,
    file_b: str,
    branch_a: str | None = None,
    branch_b: str | None = None,
) -> None:
    def file_meta(name: str, branch: str | None) -> dict:
        return {"file": name, **({"branch": branch} if branch else {})}

    output = {
        "files": {
            "old": file_meta(file_a, branch_a),
            "new": file_meta(file_b, branch_b),
        },
        "diffs": [
            {"path": e.path_str, "kind": e.kind, "old": e.old_value, "new": e.new_value}
            for e in sorted(entries, key=lambda e: e.path_str)
        ],
    }
    print(_json.dumps(output, indent=2, default=str))


# ── Shared ────────────────────────────────────────────────────────────────────


def _fmt(value: Any) -> str:
    if isinstance(value, str):
        return repr(value)
    if isinstance(value, (dict, list)):
        return _json.dumps(value, default=str, ensure_ascii=False)
    return repr(value)
