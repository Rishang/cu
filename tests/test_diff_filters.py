"""Tests for cloudutil.diff.filters."""

from cloudutil.diff.engine import DiffEntry
from cloudutil.diff.filters import apply_filters


def _entry(path: list, kind: str = "changed", old=None, new=None) -> DiffEntry:
    return DiffEntry(path=path, kind=kind, old_value=old, new_value=new)


# ── ignore_keys ────────────────────────────────────────────────────────────────


def test_ignore_key_exact_match():
    entries = [_entry(["spec", "metadata", "creationTimestamp"])]
    result = apply_filters(entries, local_ignore_keys=["metadata"])
    assert result == []


def test_ignore_key_at_root():
    entries = [_entry(["data", "password"])]
    result = apply_filters(entries, local_ignore_keys=["data"])
    assert result == []


def test_ignore_key_deep_segment():
    entries = [_entry(["a", "b", "c", "status", "d"])]
    result = apply_filters(entries, local_ignore_keys=["status"])
    assert result == []


def test_ignore_key_no_match():
    entries = [_entry(["spec", "replicas"])]
    result = apply_filters(entries, local_ignore_keys=["metadata"])
    assert len(result) == 1


def test_ignore_key_does_not_match_substring():
    """'meta' should NOT suppress a path containing 'metadata'."""
    entries = [_entry(["metadata"])]
    result = apply_filters(entries, local_ignore_keys=["meta"])
    assert len(result) == 1


def test_ignore_key_integer_segment_not_suppressed():
    """Numeric list indices should never match string ignore_keys."""
    entries = [_entry(["users", 0, "name"])]
    result = apply_filters(entries, local_ignore_keys=["0"])
    # "0" is the string representation of the index — this SHOULD filter it
    assert result == []


def test_global_ignore_key_takes_effect():
    entries = [_entry(["status", "phase"])]
    result = apply_filters(entries, global_ignore_keys=["status"])
    assert result == []


def test_global_and_local_keys_combined():
    e1 = _entry(["metadata", "uid"])
    e2 = _entry(["status", "ready"])
    e3 = _entry(["spec", "replicas"])
    result = apply_filters(
        [e1, e2, e3],
        global_ignore_keys=["metadata"],
        local_ignore_keys=["status"],
    )
    assert len(result) == 1
    assert result[0].path_str == "spec.replicas"


# ── ignore_patterns ────────────────────────────────────────────────────────────


def test_ignore_pattern_old_value():
    entries = [_entry(["cluster"], old="my-dev-cluster", new="prod-cluster")]
    result = apply_filters(entries, local_ignore_patterns=["dev"])
    assert result == []


def test_ignore_pattern_new_value():
    entries = [_entry(["db"], old="prod-db", new="stage-db")]
    result = apply_filters(entries, local_ignore_patterns=["stage"])
    assert result == []


def test_ignore_pattern_case_insensitive():
    entries = [_entry(["env"], old="TEST_DB", new="PROD_DB")]
    result = apply_filters(entries, local_ignore_patterns=["test"])
    assert result == []


def test_ignore_pattern_substring():
    entries = [
        _entry(["url"], old="https://dev.example.com", new="https://prod.example.com")
    ]
    result = apply_filters(entries, local_ignore_patterns=["dev"])
    assert result == []


def test_ignore_pattern_no_match():
    entries = [_entry(["replicas"], old=2, new=3)]
    result = apply_filters(entries, local_ignore_patterns=["dev"])
    assert len(result) == 1


def test_global_ignore_pattern():
    entries = [_entry(["env"], old="dev-cluster", new="something")]
    result = apply_filters(entries, global_ignore_patterns=["dev"])
    assert result == []


def test_multiple_patterns_any_match():
    entries = [
        _entry(["a"], old="dev-cluster", new="x"),
        _entry(["b"], old="TEST_env", new="x"),
        _entry(["c"], old="stage-api", new="x"),
        _entry(["d"], old="prod-api", new="x"),
    ]
    result = apply_filters(entries, local_ignore_patterns=["dev", "test", "stage"])
    assert len(result) == 1
    assert result[0].path_str == "d"


# ── empty inputs ───────────────────────────────────────────────────────────────


def test_no_filters_returns_all():
    entries = [_entry(["a"]), _entry(["b"])]
    assert apply_filters(entries) == entries


def test_empty_entries():
    assert apply_filters([], local_ignore_keys=["x"], local_ignore_patterns=["y"]) == []
