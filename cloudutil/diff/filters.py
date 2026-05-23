"""Filter diff entries using ignore_keys and ignore_patterns rules."""

from collections.abc import Sequence
from typing import Any

from .engine import DiffEntry


def apply_filters(
    entries: list[DiffEntry],
    global_ignore_keys: Sequence[str] = (),
    local_ignore_keys: Sequence[str] = (),
    global_ignore_patterns: Sequence[str] = (),
    local_ignore_patterns: Sequence[str] = (),
) -> list[DiffEntry]:
    """Return entries that pass all ignore rules.

    Filtering order (first match suppresses the entry):
      1. global_ignore_keys  2. local_ignore_keys
      3. global_ignore_patterns  4. local_ignore_patterns
    """
    keys = (*global_ignore_keys, *local_ignore_keys)
    patterns = (*global_ignore_patterns, *local_ignore_patterns)

    return [
        e
        for e in entries
        if not (keys and _path_has_key(e, keys))
        and not (patterns and _value_matches(e, patterns))
    ]


def _path_has_key(entry: DiffEntry, ignore_keys: Sequence[str]) -> bool:
    return any(str(seg) in ignore_keys for seg in entry.path)


def _value_matches(entry: DiffEntry, patterns: Sequence[str]) -> bool:
    lower = [p.lower() for p in patterns]

    def hit(v: Any) -> bool:
        return v is not None and any(p in str(v).lower() for p in lower)

    return hit(entry.old_value) or hit(entry.new_value)
