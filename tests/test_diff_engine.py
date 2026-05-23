"""Tests for cloudutil.diff.engine."""

from cloudutil.diff.engine import DiffEntry, compute_diff


def _kinds(entries: list[DiffEntry]) -> set[str]:
    return {e.kind for e in entries}


def _paths(entries: list[DiffEntry]) -> set[str]:
    return {e.path_str for e in entries}


# ── No differences ─────────────────────────────────────────────────────────────


def test_identical_dicts():
    assert compute_diff({"a": 1, "b": 2}, {"a": 1, "b": 2}) == []


def test_identical_nested():
    a = {"spec": {"replicas": 3, "image": "nginx"}}
    assert compute_diff(a, a) == []


def test_order_independent_dicts():
    """Key order should not matter."""
    assert compute_diff({"b": 2, "a": 1}, {"a": 1, "b": 2}) == []


def test_order_independent_lists():
    """List normalization makes reordered lists equal."""
    assert compute_diff([{"a": 2, "z": 1}], [{"z": 1, "a": 2}]) == []


# ── Added / removed ────────────────────────────────────────────────────────────


def test_added_key():
    entries = compute_diff({"a": 1}, {"a": 1, "b": 2})
    assert len(entries) == 1
    assert entries[0].kind == "added"
    assert entries[0].path_str == "b"
    assert entries[0].new_value == 2
    assert entries[0].old_value is None


def test_removed_key():
    entries = compute_diff({"a": 1, "b": 2}, {"a": 1})
    assert len(entries) == 1
    assert entries[0].kind == "removed"
    assert entries[0].path_str == "b"
    assert entries[0].old_value == 2
    assert entries[0].new_value is None


def test_added_nested_key():
    a = {"spec": {"replicas": 1}}
    b = {"spec": {"replicas": 1, "image": "nginx"}}
    entries = compute_diff(a, b)
    assert len(entries) == 1
    assert entries[0].path_str == "spec.image"
    assert entries[0].kind == "added"


# ── Changed values ─────────────────────────────────────────────────────────────


def test_changed_value():
    entries = compute_diff({"replicas": 2}, {"replicas": 3})
    assert len(entries) == 1
    assert entries[0].kind == "changed"
    assert entries[0].old_value == 2
    assert entries[0].new_value == 3


def test_changed_nested_value():
    a = {"spec": {"template": {"metadata": {"labels": {"app": "v1"}}}}}
    b = {"spec": {"template": {"metadata": {"labels": {"app": "v2"}}}}}
    entries = compute_diff(a, b)
    assert len(entries) == 1
    assert entries[0].path_str == "spec.template.metadata.labels.app"
    assert entries[0].kind == "changed"


def test_path_str_with_list_index():
    entries = compute_diff({"users": ["alice", "bob"]}, {"users": ["alice", "carol"]})
    paths = _paths(entries)
    assert "users[1]" in paths


# ── Type changes ───────────────────────────────────────────────────────────────


def test_type_changed_string_to_int():
    entries = compute_diff({"port": "8080"}, {"port": 8080})
    assert len(entries) == 1
    assert entries[0].kind == "type_changed"


def test_type_changed_dict_to_list():
    entries = compute_diff({"data": {"key": "val"}}, {"data": ["item"]})
    assert len(entries) == 1
    assert entries[0].kind == "type_changed"


def test_int_float_compatible():
    """int and float with same value should not flag as type_changed."""
    entries = compute_diff({"v": 1}, {"v": 1.0})
    assert len(entries) == 0


# ── Lists ──────────────────────────────────────────────────────────────────────


def test_list_item_added():
    entries = compute_diff({"items": ["a", "b"]}, {"items": ["a", "b", "c"]})
    assert any(e.kind == "added" for e in entries)


def test_list_item_removed():
    entries = compute_diff({"items": ["a", "b", "c"]}, {"items": ["a", "b"]})
    assert any(e.kind == "removed" for e in entries)


def test_empty_dict_vs_populated():
    entries = compute_diff({}, {"a": 1, "b": 2})
    assert len(entries) == 2
    assert all(e.kind == "added" for e in entries)


def test_null_value_changed():
    entries = compute_diff({"key": None}, {"key": "value"})
    assert len(entries) == 1
    assert entries[0].kind == "type_changed"


def test_null_to_null_no_diff():
    assert compute_diff({"key": None}, {"key": None}) == []


# ── path_str format ────────────────────────────────────────────────────────────


def test_path_str_simple():
    e = DiffEntry(path=["spec"], kind="changed", old_value=1, new_value=2)
    assert e.path_str == "spec"


def test_path_str_nested():
    e = DiffEntry(
        path=["spec", "template", "labels"], kind="changed", old_value=1, new_value=2
    )
    assert e.path_str == "spec.template.labels"


def test_path_str_with_index():
    e = DiffEntry(
        path=["users", 2, "name"], kind="changed", old_value="a", new_value="b"
    )
    assert e.path_str == "users[2].name"


def test_path_str_root():
    e = DiffEntry(path=[], kind="changed", old_value=1, new_value=2)
    assert e.path_str == "(root)"
