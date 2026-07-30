"""Token-pattern helpers backing the diff command's ignore_patterns rules."""

from __future__ import annotations

import re
from difflib import SequenceMatcher
from typing import Any, Iterable


type CompiledPatterns = list[re.Pattern[str]]


def compile_patterns(patterns: Iterable[str]) -> CompiledPatterns:
    """Compile comma-separated tokens into case-insensitive, boundary-aware patterns."""
    tokens = [
        token.strip()
        for pattern in patterns
        for token in pattern.split(",")
        if token.strip()
    ]
    return [
        re.compile(
            r"(?<![A-Za-z0-9])" + re.escape(token) + r"(?![A-Za-z0-9])",
            re.IGNORECASE,
        )
        for token in tokens
    ]


def _strip_patterns(compiled: CompiledPatterns, value: str) -> str:
    for pattern in compiled:
        value = pattern.sub("", value)
    return value


def values_similar_after_stripping(
    compiled: CompiledPatterns, left: Any, right: Any, *, threshold: float
) -> bool:
    """Compare values after removing configured tokens; absent values count as empty."""

    def stringify(value: Any) -> str:
        return "" if value is None else str(value)

    return (
        SequenceMatcher(
            None,
            _strip_patterns(compiled, stringify(left)),
            _strip_patterns(compiled, stringify(right)),
        ).ratio()
        >= threshold
    )
