"""Structural diff engine: computes path-aware differences between two objects."""

from dataclasses import dataclass
from typing import Any, Literal

from .normalize import normalize

DiffKind = Literal["added", "removed", "changed", "type_changed"]


@dataclass
class DiffEntry:
    """A single structural difference at a specific path."""

    path: list[str | int]
    kind: DiffKind
    old_value: Any = None
    new_value: Any = None

    @property
    def path_str(self) -> str:
        """Human-readable dot/bracket path, e.g. 'spec.containers[0].image'."""
        parts: list[str] = []
        for seg in self.path:
            if isinstance(seg, int):
                parts.append(f"[{seg}]")
            elif parts:
                parts.append(f".{seg}")
            else:
                parts.append(seg)
        return "".join(parts) or "(root)"


def compute_diff(a: Any, b: Any) -> list[DiffEntry]:
    """Normalize both sides then compute a recursive structural diff."""
    return _diff(normalize(a), normalize(b), [])


def _is_numeric(v: Any) -> bool:
    return isinstance(v, (int, float)) and not isinstance(v, bool)


def _diff(a: Any, b: Any, path: list[str | int]) -> list[DiffEntry]:
    # Type mismatch — treat int/float as compatible numerics
    if type(a) is not type(b) and not (_is_numeric(a) and _is_numeric(b)):
        return [DiffEntry(path=path, kind="type_changed", old_value=a, new_value=b)]

    if isinstance(a, dict):
        entries = []
        for key in sorted(set(a) | set(b)):
            child = path + [key]
            if key not in a:
                entries.append(DiffEntry(path=child, kind="added", new_value=b[key]))
            elif key not in b:
                entries.append(DiffEntry(path=child, kind="removed", old_value=a[key]))
            else:
                entries.extend(_diff(a[key], b[key], child))
        return entries

    if isinstance(a, list):
        entries = []
        for i in range(max(len(a), len(b))):
            child = path + [i]
            if i >= len(a):
                entries.append(DiffEntry(path=child, kind="added", new_value=b[i]))
            elif i >= len(b):
                entries.append(DiffEntry(path=child, kind="removed", old_value=a[i]))
            else:
                entries.extend(_diff(a[i], b[i], child))
        return entries

    if a != b:
        return [DiffEntry(path=path, kind="changed", old_value=a, new_value=b)]

    return []
