"""Shared token-pattern helpers for the diff command implementations."""

from __future__ import annotations

import re
from difflib import SequenceMatcher
from typing import Any, Iterable


type CompiledPatterns = list[re.Pattern[str]]


def compile_patterns(
    patterns: Iterable[str], *, split_commas: bool = False, ignore_case: bool = False
) -> CompiledPatterns:
    """Compile boundary-aware token patterns with the caller's input semantics."""
    tokens = (
        [
            token.strip()
            for pattern in patterns
            for token in pattern.split(",")
            if token.strip()
        ]
        if split_commas
        else list(patterns)
    )
    flags = re.IGNORECASE if ignore_case else re.NOFLAG
    return [
        re.compile(
            r"(?<![A-Za-z0-9])" + re.escape(token) + r"(?![A-Za-z0-9])",
            flags,
        )
        for token in tokens
    ]


def any_pattern_matches(
    compiled: CompiledPatterns, *values: Any, none_matches: bool = False
) -> bool:
    """Return whether a compiled pattern matches any supplied value."""
    return any(
        pattern.search(str(value))
        for pattern in compiled
        for value in values
        if none_matches or value is not None
    )


def strip_patterns(compiled: CompiledPatterns, value: str) -> str:
    """Remove every compiled token from a string."""
    for pattern in compiled:
        value = pattern.sub("", value)
    return value


def values_similar_after_stripping(
    compiled: CompiledPatterns,
    left: Any,
    right: Any,
    *,
    threshold: float,
    none_as_empty: bool = True,
) -> bool:
    """Compare values after removing configured tokens.

    ``none_as_empty`` preserves the existing difference between the modern
    diff command (which treats absent values as empty) and legacy ydiff
    rendering (which stringifies them as ``"None"``).
    """

    def stringify(value: Any) -> str:
        return "" if none_as_empty and value is None else str(value)

    return (
        SequenceMatcher(
            None,
            strip_patterns(compiled, stringify(left)),
            strip_patterns(compiled, stringify(right)),
        ).ratio()
        >= threshold
    )
