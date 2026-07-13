"""Render diff entries in unified (git-diff-style), table, or JSON format."""

import json as _json
from collections import defaultdict
from collections.abc import Callable
from difflib import SequenceMatcher
from typing import Any, Literal, NamedTuple

from rich import box
from rich.table import Table
from rich.text import Text

from cloudutil.utils import console
from .engine import DiffEntry

OutputFormat = Literal["unified", "table", "json"]
ValueStyle = Literal["default", "hcl"]
Fmt = Callable[[Any], str]

ADD, DEL, ARROW, NOTE, LABEL = "green", "red", "dim", "dim italic", "bold"
DIFF_DEL_BG, DIFF_ADD_BG = "red on #3a1414", "green on #143a1c"


class _Line(NamedTuple):
    """A single rendered diff line: symbol, key, and styled value segments."""

    sym: str
    sym_style: str
    key: str
    key_style: str
    segs: list[tuple[str, str]]  # (text, rich-style) — empty style = default


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
    value_style: ValueStyle = "default",
    ignored: list[DiffEntry] | None = None,
) -> None:
    fmt: Fmt = _fmt_hcl if value_style == "hcl" else _fmt
    match format:
        case "table":
            _render_table(entries, file_a, file_b, branch_a, branch_b, fmt, ignored)
        case "json":
            _render_json(entries, file_a, file_b, branch_a, branch_b)
        case _:
            _render_unified(
                entries, file_a, file_b, branch_a, branch_b, color, fmt, ignored
            )


# ── Unified ───────────────────────────────────────────────────────────────────


def _render_ignored_section(ignored: list[DiffEntry]) -> None:
    if not ignored:
        return
    console.print(f"[dim]⊘  Ignored ({len(ignored)}) — matched ignore rules[/dim]")
    for e in sorted(ignored, key=lambda e: e.path_str):
        console.print(f"[dim]   ~ {e.path_str}[/dim]")
    console.print()


def _render_unified(
    entries: list[DiffEntry],
    file_a: str,
    file_b: str,
    branch_a: str | None,
    branch_b: str | None,
    color: bool,
    fmt: Fmt,
    ignored: list[DiffEntry] | None = None,
) -> None:
    _print_header(file_a, file_b, branch_a, branch_b, color)

    if not entries:
        console.print()
        console.print(
            "[bold green]✓  No differences[/bold green]"
            if color
            else "✓  No differences"
        )
        _render_ignored_section(ignored or [])
        return

    groups: dict[str, list[DiffEntry]] = defaultdict(list)
    for entry in sorted(entries, key=lambda e: e.path_str):
        groups[str(entry.path[0]) if entry.path else "(root)"].append(entry)

    added = removed = changed = 0

    for section, group in sorted(groups.items()):
        lines = _build_lines(group, section, fmt)
        n = len(lines)
        label = f"({n} change{'s' if n != 1 else ''})"
        console.print()
        if color:
            console.print(f"[bold cyan]@@ {section} @@[/bold cyan]  [dim]{label}[/dim]")
        else:
            console.print(f"@@ {section} @@  {label}")

        pad = max((len(ln.key) for ln in lines if ln.key), default=0)
        for ln in lines:
            if ln.sym == "+":
                added += 1
            elif ln.sym == "-":
                removed += 1
            else:
                changed += 1
            _print_line(ln, pad, color)

    _render_ignored_section(ignored or [])
    console.print()
    console.print(_summary_table(added, removed, changed))


def _print_header(
    file_a: str, file_b: str, branch_a: str | None, branch_b: str | None, color: bool
) -> None:
    def tag(branch: str | None) -> str:
        return f"  ({branch})" if branch else ""

    if color:
        console.print(f"[bold red]--- a/{file_a}[/bold red][dim]{tag(branch_a)}[/dim]")
        console.print(
            f"[bold green]+++ b/{file_b}[/bold green][dim]{tag(branch_b)}[/dim]"
        )
    else:
        console.print(f"--- a/{file_a}{tag(branch_a)}")
        console.print(f"+++ b/{file_b}{tag(branch_b)}")


def _print_line(ln: _Line, pad: int, color: bool) -> None:
    if not color:
        col = f"{ln.key}:".ljust(pad + 1) if ln.key else ""
        plain = "".join(t for t, _ in ln.segs)
        console.print(f"{ln.sym}  {col}  {plain}" if col else f"{ln.sym}  {plain}")
        return

    text = Text()
    text.append(f"{ln.sym}  ", style=ln.sym_style)
    if ln.key:
        text.append(f"{ln.key}:".ljust(pad + 1), style=ln.key_style)
        text.append("  ")
    for t, style in ln.segs:
        text.append(t, style=style)
    console.print(text)


def _summary_table(added: int, removed: int, changed: int) -> Table:
    t = Table(
        box=box.SIMPLE_HEAD,
        show_header=True,
        header_style="dim",
        padding=(0, 2),
        expand=False,
        show_edge=False,
    )
    t.add_column("+  added", style="bold green", justify="right")
    t.add_column("-  removed", style="bold red", justify="right")
    t.add_column("~  changed", style="bold yellow", justify="right")
    t.add_row(str(added), str(removed), str(changed))
    return t


def _build_lines(group: list[DiffEntry], section: str, fmt: Fmt) -> list[_Line]:
    """Convert DiffEntry objects into styled _Line tuples."""
    lines: list[_Line] = []
    for entry in group:
        rel = _rel_key(entry.path_str, section)
        match entry.kind:
            case "added":
                lines.extend(
                    _added_line(k, v, fmt) for k, v in _expand(rel, entry.new_value)
                )
            case "removed":
                lines.extend(
                    _removed_line(k, v, fmt) for k, v in _expand(rel, entry.old_value)
                )
            case "changed":
                lines.extend(
                    _changed_lines(
                        rel, entry.old_value, entry.new_value, note="", fmt=fmt
                    )
                )
            case "type_changed":
                note = f"  ({type(entry.old_value).__name__} → {type(entry.new_value).__name__})"
                lines.extend(
                    _changed_lines(
                        rel, entry.old_value, entry.new_value, note=note, fmt=fmt
                    )
                )
    return lines


def _added_line(key: str, value: Any, fmt: Fmt) -> _Line:
    return _Line("+", ADD, key, ADD, [(fmt(value), ADD)])


def _removed_line(key: str, value: Any, fmt: Fmt) -> _Line:
    return _Line("-", DEL, key, DEL, [(fmt(value), DEL)])


def _changed_lines(
    key: str, old_v: Any, new_v: Any, note: str, fmt: Fmt
) -> list[_Line]:
    if _is_scalar(old_v) and _is_scalar(new_v):
        segs: list[tuple[str, str]] = [
            (fmt(old_v), DEL),
            (" → ", ARROW),
            (fmt(new_v), ADD),
        ]
        if note:
            segs.append((note, NOTE))
        return [_Line("~", "yellow", key, LABEL, segs)]

    # Non-scalar: emit a removed line + an added line, with the note attached to the removed side.
    rem_segs: list[tuple[str, str]] = [(fmt(old_v), DEL)]
    if note:
        rem_segs.append((note, NOTE))
    return [
        _Line("-", DEL, key, DEL, rem_segs),
        _Line("+", ADD, key, ADD, [(fmt(new_v), ADD)]),
    ]


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


def _diff_pair(old_str: str, new_str: str) -> tuple[Text, Text]:
    """Highlight the exact differing spans between old/new, red/green bg."""
    old, new = Text(style="red"), Text(style="green")
    for op, i1, i2, j1, j2 in SequenceMatcher(a=old_str, b=new_str).get_opcodes():
        if op == "equal":
            old.append(old_str[i1:i2])
            new.append(new_str[j1:j2])
        else:
            old.append(old_str[i1:i2], style=DIFF_DEL_BG)
            new.append(new_str[j1:j2], style=DIFF_ADD_BG)
    return old, new


# ── Table ─────────────────────────────────────────────────────────────────────


def _render_table(
    entries: list[DiffEntry],
    file_a: str,
    file_b: str,
    branch_a: str | None,
    branch_b: str | None,
    fmt: Fmt,
    ignored: list[DiffEntry] | None = None,
) -> None:
    if not entries:
        _render_ignored_section(ignored or [])
        console.print("[bold green]✓  No differences[/bold green]")
        return

    def col_label(prefix: str, name: str, branch: str | None) -> Text:
        color = "red" if prefix == "−" else "green"
        t = Text()
        t.append(prefix, style=f"bold {color}")
        t.append(f" {name}", style="bold")
        if branch:
            t.append(f" ({branch})", style="dim")
        return t

    t = Table(
        box=box.ROUNDED,
        show_header=True,
        header_style="bold cyan",
        padding=(0, 2),
        row_styles=["", "on grey7"],
    )
    t.add_column("", width=3, no_wrap=True, justify="center")
    t.add_column("Path", style="bold", no_wrap=True)
    t.add_column(col_label("−", file_a, branch_a), style="red", overflow="fold")
    t.add_column(col_label("+", file_b, branch_b), style="green", overflow="fold")

    added = removed = changed = 0
    dash = Text("—", style="dim")
    for entry in sorted(entries, key=lambda e: e.path_str):
        sym, style = _KIND[entry.kind]
        if entry.kind == "added":
            added += 1
            old, new = dash, Text(fmt(entry.new_value), style="green")
        elif entry.kind == "removed":
            removed += 1
            old, new = Text(fmt(entry.old_value), style="red"), dash
        else:
            changed += 1
            old, new = _diff_pair(fmt(entry.old_value), fmt(entry.new_value))
        t.add_row(Text(sym, style=f"bold {style}"), entry.path_str, old, new)

    _render_ignored_section(ignored or [])
    console.print(t)
    console.print()
    console.print(_summary_table(added, removed, changed))


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


def _fmt_hcl(value: Any) -> str:
    """Format a value as an HCL literal for display.

    python-hcl2 preserves string literals with their HCL double-quote chars
    embedded (e.g. 'us-east-1' in HCL is stored as '"us-east-1"' in Python).
    Pass those through unchanged; only bare strings get wrapped in quotes.
    """
    if value is None:
        return "null"
    if isinstance(value, bool):  # must precede int — bool is a subclass of int
        return "true" if value else "false"
    if isinstance(value, str):
        # hcl2 preserves HCL string literals with embedded quote chars
        if len(value) >= 2 and value[0] == '"' and value[-1] == '"':
            return value
        return f'"{value}"'
    if isinstance(value, (int, float)):
        return str(value)
    if isinstance(value, list):
        items = ", ".join(_fmt_hcl(v) for v in value)
        return f"[{items}]"
    if isinstance(value, dict):
        pairs = " ".join(
            f"{k} = {_fmt_hcl(v)}"
            for k, v in sorted(value.items())
            if k != "__is_block__"  # hcl2 metadata marker — not a real attribute
        )
        return "{ " + pairs + " }" if pairs else "{}"
    return str(value)
