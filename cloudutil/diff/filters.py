"""Filter diff entries using ignore_keys, ignore_patterns, and JMESPath query rules."""

from collections.abc import Sequence
from typing import Any

import jmespath

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


def apply_query(entries: list[DiffEntry], query: str) -> list[DiffEntry]:
    """Filter diff entries using a JMESPath expression or a bare path prefix.

    Two modes, selected automatically:

    Path prefix (query does NOT start with '['):
        Keeps entries whose path equals or is nested under the given prefix.
        resource[0].aws_instance.web   → all diffs inside that block
        spec.replicas                  → exact path match
        spec                           → all diffs inside spec

    JMESPath filter expression (query starts with '['):
        Applied to list[{path, kind, old, new}]; must return a list of
        objects still carrying 'path' and 'kind' fields.
        [?kind=='changed']
        [?kind!='changed']
        [?contains(path, 'resources')]
        [?path=='spec.replicas']
        [?old=='t2.micro']
    """
    if not query.lstrip().startswith("["):
        return _prefix_filter(entries, query.rstrip("."))

    try:
        expr = jmespath.compile(query)
    except jmespath.exceptions.JMESPathError as exc:
        raise ValueError(f"Invalid JMESPath query {query!r}: {exc}") from exc

    data = [
        {"path": e.path_str, "kind": e.kind, "old": e.old_value, "new": e.new_value}
        for e in entries
    ]
    result = expr.search(data)

    if result is None:
        return []
    if not isinstance(result, list):
        result = [result]

    keep = {
        (d["path"], d["kind"])
        for d in result
        if isinstance(d, dict) and "path" in d and "kind" in d
    }
    return [e for e in entries if (e.path_str, e.kind) in keep]


def _prefix_filter(entries: list[DiffEntry], prefix: str) -> list[DiffEntry]:
    return [
        e
        for e in entries
        if e.path_str == prefix
        or e.path_str.startswith(prefix + ".")
        or e.path_str.startswith(prefix + "[")
    ]


def _path_has_key(entry: DiffEntry, ignore_keys: Sequence[str]) -> bool:
    return any(str(seg) in ignore_keys for seg in entry.path)


def _value_matches(entry: DiffEntry, patterns: Sequence[str]) -> bool:
    lower = [p.lower() for p in patterns]

    def hit(v: Any) -> bool:
        return v is not None and any(p in str(v).lower() for p in lower)

    return hit(entry.old_value) or hit(entry.new_value)
