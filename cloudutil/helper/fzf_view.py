"""Abstract base class for interactive fzf-powered list-and-view workflows."""

from __future__ import annotations

import json
import sys
import subprocess
from abc import ABC, abstractmethod
from typing import Generic, List, Optional, TypeVar
from cloudutil.utils import console
from rich.console import Console

# T is the domain object produced by list_items() and consumed by display_item().
T = TypeVar("T")


def _run_fzf(items: List[str], multi_select: bool = True) -> tuple[int, List[str], str]:
    """Run fzf and return ``(returncode, selected_lines, stderr)``.

    Callers own presentation so the legacy selector and ``FzfView`` can retain
    their distinct messages while sharing the process invocation.
    """
    if not items:
        return 0, [], ""

    command = ["fzf", "-e"]
    if multi_select:
        command.append("-m")

    try:
        result = subprocess.run(
            command,
            input="\n".join(items),
            capture_output=True,
            text=True,
            check=False,
        )
    except FileNotFoundError:
        return 127, [], "Command not found: fzf"

    return (
        result.returncode,
        [line for line in result.stdout.strip().splitlines() if line],
        result.stderr.strip(),
    )


class FzfView(ABC, Generic[T]):
    """
    Abstract base for interactive fzf list-and-view workflows.

    Subclasses implement three methods:

    - ``list_items()``    — fetch the full set of domain objects.
    - ``item_label(item)``— convert a domain object to the string shown in fzf.
    - ``display_item(item)`` — render a selected domain object to the terminal.

    Then call ``run()`` to execute the full workflow.

    Example subclass::

        class MyView(FzfView[MyThing]):
            def list_items(self) -> List[MyThing]: ...
            def item_label(self, item: MyThing) -> str: ...
            def display_item(self, item: MyThing) -> None: ...

        MyView().run()
    """

    # Override in subclasses to allow/disallow multi-select.
    multi_select: bool = True

    # Human-readable name used in status/error messages.
    item_type_name: str = "item"

    @abstractmethod
    def list_items(self) -> List[T]:
        """Return all available domain objects to present in fzf."""
        pass

    @abstractmethod
    def item_label(self, item: T) -> str:
        """Return the fzf display string for a single domain object."""
        pass

    @abstractmethod
    def display_item(self, item: T) -> dict[str, str]:
        """Return display payload for one selected domain object."""
        pass

    def display_selection(self, items: List[T]) -> None:
        """Render selected domain objects as a single JSON payload."""
        payload: dict[str, str] = {}
        for item in items:
            item_payload = self.display_item(item)
            if item_payload is not None:
                payload.update(item_payload)
        if payload:
            self.print_json(payload)

    # ── Optional hooks ────────────────────────────────────────────────────────

    def before_fzf(self, items: List[T]) -> None:
        """Called after listing, before launching fzf. Override to add context."""
        console.print(
            f"[*] Found {len(items)} {self.item_type_name}(s). "
            "Opening fzf for selection..."
        )

    def resolve_selection(self, label: str, items: List[T]) -> Optional[T]:
        """
        Map a fzf-selected label string back to a domain object.

        Default implementation does a linear scan by ``item_label``.
        Override for O(1) lookup if needed.
        """
        for item in items:
            if self.item_label(item) == label:
                return item
        return None

    def print_json(self, payload: object) -> None:
        raw = json.dumps(payload, default=str)
        if sys.stdout.isatty():
            console_out = Console()  # stdout, not stderr
            console_out.print_json(raw)
        else:
            print(raw)

    # ── Entry point ───────────────────────────────────────────────────────────

    def run(self) -> None:
        """Execute the full list → fzf → display workflow."""
        items = self.list_items()

        if not items:
            console.print(f"[yellow][!] No {self.item_type_name}s found.[/yellow]")
            return

        self.before_fzf(items)

        labels = [self.item_label(item) for item in items]
        returncode, selected_labels, stderr = _run_fzf(
            labels, multi_select=self.multi_select
        )
        if returncode == 127:
            console.print(
                "[bold red][!] ERROR: fzf not found. Please install fzf.[/bold red]"
            )
            return
        if returncode not in (0, 1):
            console.print(
                f"[bold red][!] ERROR: fzf exited with code {returncode}: "
                f"{stderr}[/bold red]"
            )
            return
        if not selected_labels:
            console.print("[yellow][!] No selection made.[/yellow]")
            return

        selected_items = []
        for label in selected_labels:
            resolved = self.resolve_selection(label, items)
            if resolved is None:
                console.print(
                    f"[yellow][!] Could not resolve selection: {label!r}[/yellow]"
                )
                continue
            selected_items.append(resolved)

        self.display_selection(selected_items)
