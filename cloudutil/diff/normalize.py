"""Recursive normalization of structured data for deterministic comparison."""

from typing import Any


def normalize(obj: Any) -> Any:
    """Recursively normalize an object for structural comparison.

    - Dicts: keys sorted alphabetically at every level.
    - Lists: items recursively normalized; order is preserved.
    - Primitives: returned as-is.
    """
    if isinstance(obj, dict):
        return {k: normalize(v) for k, v in sorted(obj.items())}
    if isinstance(obj, list):
        return [normalize(item) for item in obj]
    return obj
