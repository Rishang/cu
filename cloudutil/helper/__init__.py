"""cloudutil.helper — shared utilities re-exported for convenience."""

from cloudutil.helper.fzf_view import FzfView

# Re-export the legacy helpers that existing modules import from cloudutil.helper
# so nothing breaks while callers are migrated to the new package layout.
from cloudutil.utils import ShellRunner, shell, resolve_env_variable

from cloudutil.utils import shell as _shell
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

    fzf_cmd = ["fzf", "-e"]
    if multi_select:
        fzf_cmd.append("-m")

    success, stdout, stderr = _shell.run_command(fzf_cmd, input_text="\n".join(items))

    if not success:
        if "Command not found" in stderr and "fzf" in stderr:
            _console.print(
                f"[bold red][!] ERROR: fzf not found. Please install fzf for "
                f"interactive {service_name} selection.[/bold red]"
            )
        else:
            _console.print(
                f"[bold red][!] ERROR: fzf selection failed: {stderr}[/bold red]"
            )
        return []

    selected = stdout.strip().splitlines()

    if not selected:
        if not quiet:
            _console.print("[yellow][!] No selection made.[/yellow]")
        return []

    return selected


__all__ = [
    "FzfView",
    "ShellRunner",
    "shell",
    "fzf_select",
    "resolve_env_variable",
]
