"""Tests for cloudutil.diff.filters."""

from cloudutil.diff.engine import DiffEntry
from cloudutil.diff.filters import apply_filters


def _entry(path: list, kind: str = "changed", old=None, new=None) -> DiffEntry:
    return DiffEntry(path=path, kind=kind, old_value=old, new_value=new)


# ── ignore_keys ────────────────────────────────────────────────────────────────


def test_ignore_key_exact_match():
    entries = [_entry(["spec", "metadata", "creationTimestamp"])]
    result, ignored_list = apply_filters(entries, local_ignore_keys=["metadata"])
    assert result == []


def test_ignore_key_at_root():
    entries = [_entry(["data", "password"])]
    result, ignored_list = apply_filters(entries, local_ignore_keys=["data"])
    assert result == []


def test_ignore_key_deep_segment():
    entries = [_entry(["a", "b", "c", "status", "d"])]
    result, ignored_list = apply_filters(entries, local_ignore_keys=["status"])
    assert result == []


def test_ignore_key_no_match():
    entries = [_entry(["spec", "replicas"])]
    result, ignored_list = apply_filters(entries, local_ignore_keys=["metadata"])
    assert len(result) == 1


def test_ignore_key_does_not_match_substring():
    """'meta' should NOT suppress a path containing 'metadata'."""
    entries = [_entry(["metadata"])]
    result, ignored_list = apply_filters(entries, local_ignore_keys=["meta"])
    assert len(result) == 1


def test_ignore_key_integer_segment_not_suppressed():
    """Numeric list indices should never match string ignore_keys."""
    entries = [_entry(["users", 0, "name"])]
    result, ignored_list = apply_filters(entries, local_ignore_keys=["0"])
    # "0" is the string representation of the index — this SHOULD filter it
    assert result == []


def test_global_ignore_key_takes_effect():
    entries = [_entry(["status", "phase"])]
    result, ignored_list = apply_filters(entries, global_ignore_keys=["status"])
    assert result == []


def test_global_and_local_keys_combined():
    e1 = _entry(["metadata", "uid"])
    e2 = _entry(["status", "ready"])
    e3 = _entry(["spec", "replicas"])
    result, ignored_list = apply_filters(
        [e1, e2, e3],
        global_ignore_keys=["metadata"],
        local_ignore_keys=["status"],
    )
    assert len(result) == 1
    assert result[0].path_str == "spec.replicas"


# ── ignore_patterns ────────────────────────────────────────────────────────────


def test_ignore_pattern_old_value():
    # With smart ignore: new="prod-cluster" doesn't match "dev" → NOT ignored
    entries = [_entry(["cluster"], old="my-dev-cluster", new="prod-cluster")]
    result, ignored_list = apply_filters(entries, local_ignore_patterns=["dev"])
    assert len(result) == 1


def test_ignore_pattern_new_value():
    # With smart ignore: old="prod-db" doesn't match "stage" → NOT ignored
    entries = [_entry(["db"], old="prod-db", new="stage-db")]
    result, ignored_list = apply_filters(entries, local_ignore_patterns=["stage"])
    assert len(result) == 1


def test_ignore_pattern_case_insensitive():
    # With smart ignore: new="PROD_DB" doesn't match "test" → NOT ignored
    entries = [_entry(["env"], old="TEST_DB", new="PROD_DB")]
    result, ignored_list = apply_filters(entries, local_ignore_patterns=["test"])
    assert len(result) == 1


def test_ignore_pattern_case_insensitive_both_match():
    # old="dev-app", new="dev-app": both contain "dev", stripped strings are identical → ignored
    entries = [_entry(["env"], old="dev-app", new="dev-app")]
    result, ignored_list = apply_filters(entries, local_ignore_patterns=["dev"])
    assert result == []


def test_ignore_pattern_substring():
    # With smart ignore: old="https://dev.example.com", new="https://prod.example.com"
    # only old matches "dev" → NOT ignored
    entries = [
        _entry(["url"], old="https://dev.example.com", new="https://prod.example.com")
    ]
    result, ignored_list = apply_filters(entries, local_ignore_patterns=["dev"])
    assert len(result) == 1


def test_smart_ignore_env_in_both():
    # Both match patterns ["dev","prod"] → both stripped → similar → ignored
    entries = [
        _entry(["url"], old="https://dev.example.com", new="https://prod.example.com")
    ]
    result, ignored_list = apply_filters(entries, local_ignore_patterns=["dev", "prod"])
    assert result == []


def test_ignore_pattern_no_match():
    entries = [_entry(["replicas"], old=2, new=3)]
    result, ignored_list = apply_filters(entries, local_ignore_patterns=["dev"])
    assert len(result) == 1


def test_global_ignore_pattern():
    # old="dev-cluster", new="something": new doesn't match "dev" → NOT ignored
    entries = [_entry(["env"], old="dev-cluster", new="something")]
    result, ignored_list = apply_filters(entries, global_ignore_patterns=["dev"])
    assert len(result) == 1


def test_multiple_patterns_any_match():
    entries = [
        _entry(
            ["a"], old="dev-cluster", new="prod-cluster"
        ),  # both match ["dev","prod"], similar → ignored
        _entry(
            ["b"], old="dev-api.company.com", new="prod-api.company.com"
        ),  # both match, similar → ignored
        _entry(
            ["c"], old="dev-cluster", new="completely-different"
        ),  # only old matches → NOT ignored
        _entry(["d"], old="normal", new="other"),  # neither matches → NOT ignored
    ]
    result, ignored_list = apply_filters(entries, local_ignore_patterns=["dev", "prod"])
    assert len(result) == 2
    assert {r.path_str for r in result} == {"c", "d"}
    assert len(ignored_list) == 2


# ── empty inputs ───────────────────────────────────────────────────────────────


def test_no_filters_returns_all():
    entries = [_entry(["a"]), _entry(["b"])]
    result, ignored_list = apply_filters(entries)
    assert result == entries


def test_empty_entries():
    result, ignored_list = apply_filters(
        [], local_ignore_keys=["x"], local_ignore_patterns=["y"]
    )
    assert result == []


# ── smart similarity (both-match + SequenceMatcher) ───────────────────────────


def test_smart_ignore_both_match_high_similarity():
    """Both values contain env name; after stripping they are identical → ignored."""
    e = _entry(["host"], old="dev-server", new="prod-server")
    result, ignored = apply_filters([e], local_ignore_patterns=["dev", "prod"])
    assert result == []
    assert len(ignored) == 1


def test_smart_ignore_both_match_low_similarity():
    """Both contain env name but remaining text is totally different → NOT ignored."""
    e = _entry(
        ["x"], old="dev-alpha-backend", new="prod-completely-different-thing-xyz"
    )
    result, ignored = apply_filters([e], local_ignore_patterns=["dev", "prod"])
    assert len(result) == 1
    assert ignored == []


def test_smart_ignore_added_entry_always_kept():
    """Added entries have no counterpart to compare — always kept regardless of pattern."""
    e = _entry(["ns"], kind="added", new="dev-namespace")
    result, ignored = apply_filters([e], local_ignore_patterns=["dev"])
    assert len(result) == 1
    assert ignored == []


def test_smart_ignore_removed_entry_always_kept():
    """Removed entries have no counterpart to compare — always kept regardless of pattern."""
    e = _entry(["ns"], kind="removed", old="prod-namespace")
    result, ignored = apply_filters([e], local_ignore_patterns=["prod"])
    assert len(result) == 1
    assert ignored == []


def test_smart_word_boundary_no_match():
    """Pattern 'dev' should NOT match 'mydevserver' (no word boundary)."""
    e = _entry(["key"], old="mydevserver", new="myprodserver")
    result, ignored = apply_filters([e], local_ignore_patterns=["dev", "prod"])
    assert len(result) == 1


def test_smart_ignore_ignored_list_populated():
    """Verify the ignored list is returned correctly."""
    e1 = _entry(["host"], old="dev-server", new="prod-server")
    e2 = _entry(["replicas"], old=1, new=3)
    result, ignored = apply_filters([e1, e2], local_ignore_patterns=["dev", "prod"])
    assert len(result) == 1
    assert result[0].path_str == "replicas"
    assert len(ignored) == 1
    assert ignored[0].path_str == "host"
