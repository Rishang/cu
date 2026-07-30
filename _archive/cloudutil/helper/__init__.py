"""cloudutil.helper — shared utilities re-exported for convenience."""

from cloudutil.helper.fzf_view import FzfView, _run_fzf

from cloudutil.utils import console as _console
from typing import List as _List


def fzf_select(
    items: _List[str],
    service_name: str = "item",
    multi_select: bool = True,
    quiet: bool = False,
) -> _List[str]:
    """
    Interactive selection using fzf.

    Thin wrapper kept for backwards-compatibility; prefer ``FzfView`` for new code.
    """
    if not items:
        if not quiet:
            _console.print(f"[yellow][!] No {service_name}s found.[/yellow]")
        return []

    if not quiet:
        _console.print(
            f"[*] Found {len(items)} {service_name}s. Opening fzf for selection..."
        )

    returncode, selected, stderr = _run_fzf(items, multi_select=multi_select)
    if returncode != 0:
        if returncode == 127:
            _console.print(
                f"[bold red][!] ERROR: fzf not found. Please install fzf for "
                f"interactive {service_name} selection.[/bold red]"
            )
        else:
            _console.print(
                f"[bold red][!] ERROR: fzf selection failed: {stderr}[/bold red]"
            )
        return []

    if not selected:
        if not quiet:
            _console.print("[yellow][!] No selection made.[/yellow]")
        return []

    return selected


__all__ = [
    "FzfView",
    "fzf_select",
]
