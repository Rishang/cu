"""Filter diff entries using ignore_keys, ignore_patterns, and JMESPath query rules."""

from collections.abc import Sequence

import jmespath

from .engine import DiffEntry
from .patterns import CompiledPatterns, compile_patterns, values_similar_after_stripping

_SIMILARITY_THRESHOLD = 1.0


def _smart_ignore(compiled: CompiledPatterns, entry: DiffEntry) -> bool:
    """Strip all pattern tokens from both values; ignore the entry if what remains is identical.

    Logic: remove the env/marker keywords from both sides, then compare. If the
    only difference between the two values was the marker token, the stripped
    strings are equal → ignore. If something else differs → keep as a real diff.

    Added/removed entries have no counterpart to compare against, so they are
    always kept.
    """
    if entry.kind in ("added", "removed"):
        return False
    return values_similar_after_stripping(
        compiled,
        entry.old_value,
        entry.new_value,
        threshold=_SIMILARITY_THRESHOLD,
    )


def apply_filters(
    entries: list[DiffEntry],
    global_ignore_keys: Sequence[str] = (),
    local_ignore_keys: Sequence[str] = (),
    global_ignore_patterns: Sequence[str] = (),
    local_ignore_patterns: Sequence[str] = (),
) -> tuple[list[DiffEntry], list[DiffEntry]]:
    """Return (kept, ignored) entry lists after applying all ignore rules.

    Filtering order (first match suppresses the entry):
      1. global_ignore_keys  2. local_ignore_keys
      3. global_ignore_patterns  4. local_ignore_patterns
    """
    keys = (*global_ignore_keys, *local_ignore_keys)
    patterns = list((*global_ignore_patterns, *local_ignore_patterns))
    compiled = compile_patterns(patterns) if patterns else []

    kept: list[DiffEntry] = []
    ignored: list[DiffEntry] = []

    for e in entries:
        if keys and _path_has_key(e, keys):
            ignored.append(e)
        elif compiled and _smart_ignore(compiled, e):
            ignored.append(e)
        else:
            kept.append(e)

    return kept, ignored


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
