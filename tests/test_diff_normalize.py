"""Tests for cloudutil.diff.normalize."""

from cloudutil.diff.normalize import normalize


def test_dict_keys_sorted():
    result = normalize({"z": 1, "a": 2, "m": 3})
    assert list(result.keys()) == ["a", "m", "z"]


def test_nested_dict_keys_sorted():
    result = normalize({"b": {"z": 1, "a": 2}, "a": {"y": 10, "x": 5}})
    assert list(result["a"].keys()) == ["x", "y"]
    assert list(result["b"].keys()) == ["a", "z"]


def test_list_of_dicts_sorted_deterministically():
    a = normalize([{"z": 1, "a": 2}])
    b = normalize([{"a": 2, "z": 1}])
    assert a == b


def test_list_order_preserved():
    result = normalize(["banana", "apple", "cherry"])
    assert result == ["banana", "apple", "cherry"]


def test_list_of_mixed_dicts_order_preserved():
    input_data = [{"name": "Bob", "age": 25}, {"name": "Alice", "age": 30}]
    result = normalize(input_data)
    assert result[0]["name"] == "Bob"
    assert result[1]["name"] == "Alice"


def test_spec_example_list_order_equivalence():
    """The spec example: two dicts with same keys in different order must normalize equal."""
    a = normalize({"a": [{"z": 1, "a": 2}]})
    b = normalize({"a": [{"a": 2, "z": 1}]})
    assert a == b


def test_primitives_unchanged():
    assert normalize(42) == 42
    assert normalize("hello") == "hello"
    assert normalize(True) is True
    assert normalize(None) is None


def test_empty_structures():
    assert normalize({}) == {}
    assert normalize([]) == []


def test_deeply_nested():
    data = {"x": {"b": [{"z": 3, "a": 1}, {"z": 2, "a": 0}], "a": "val"}}
    result = normalize(data)
    assert list(result["x"].keys()) == ["a", "b"]
    # List order preserved; only dict keys are sorted within each item
    assert result["x"]["b"][0] == {"a": 1, "z": 3}
    assert result["x"]["b"][1] == {"a": 0, "z": 2}


def test_list_order_preserved_numbers():
    assert normalize([3, 1, 4, 1, 5, 9]) == [3, 1, 4, 1, 5, 9]


def test_null_values_preserved():
    result = normalize({"key": None, "other": "val"})
    assert result["key"] is None
