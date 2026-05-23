"""Tests for cloudutil.diff.loader."""

import textwrap
from pathlib import Path

import pytest

from cloudutil.diff.loader import load_file


@pytest.fixture()
def tmp_dir(tmp_path: Path) -> Path:
    return tmp_path


def _write(tmp_dir: Path, name: str, content: str) -> Path:
    p = tmp_dir / name
    p.write_text(textwrap.dedent(content), encoding="utf-8")
    return p


# ── JSON ───────────────────────────────────────────────────────────────────────


def test_load_json(tmp_dir):
    p = _write(tmp_dir, "a.json", '{"key": "value", "num": 42}')
    result = load_file(p)
    assert result == {"key": "value", "num": 42}


def test_load_json_nested(tmp_dir):
    p = _write(tmp_dir, "a.json", '{"a": {"b": [1, 2, 3]}}')
    assert load_file(p)["a"]["b"] == [1, 2, 3]


def test_invalid_json(tmp_dir):
    p = _write(tmp_dir, "bad.json", "{not valid json}")
    with pytest.raises(ValueError, match="Invalid JSON"):
        load_file(p)


# ── YAML ───────────────────────────────────────────────────────────────────────


def test_load_yaml(tmp_dir):
    p = _write(tmp_dir, "a.yaml", "key: value\nnum: 42\n")
    result = load_file(p)
    assert result == {"key": "value", "num": 42}


def test_load_yml_extension(tmp_dir):
    p = _write(tmp_dir, "a.yml", "x: 1\n")
    assert load_file(p) == {"x": 1}


def test_load_yaml_nested(tmp_dir):
    p = _write(tmp_dir, "nested.yaml", "spec:\n  replicas: 3\n  image: nginx\n")
    result = load_file(p)
    assert result["spec"]["replicas"] == 3


def test_invalid_yaml(tmp_dir):
    p = _write(tmp_dir, "bad.yaml", "key: [\n  - unbalanced\n")
    with pytest.raises(ValueError, match="Invalid YAML"):
        load_file(p)


# ── TOML ───────────────────────────────────────────────────────────────────────


def test_load_toml(tmp_dir):
    p = _write(tmp_dir, "a.toml", '[server]\nhost = "localhost"\nport = 8080\n')
    result = load_file(p)
    assert result["server"]["host"] == "localhost"
    assert result["server"]["port"] == 8080


def test_invalid_toml(tmp_dir):
    p = _write(tmp_dir, "bad.toml", "key = [invalid toml")
    with pytest.raises(ValueError, match="Invalid TOML"):
        load_file(p)


# ── Edge cases ─────────────────────────────────────────────────────────────────


def test_missing_file():
    with pytest.raises(FileNotFoundError, match="File not found"):
        load_file("/nonexistent/path/file.yaml")


def test_unsupported_extension(tmp_dir):
    p = _write(tmp_dir, "config.xml", "<root/>")
    with pytest.raises(ValueError, match="Unsupported format"):
        load_file(p)


def test_empty_yaml_returns_none(tmp_dir):
    p = _write(tmp_dir, "empty.yaml", "")
    result = load_file(p)
    assert result is None


def test_empty_json_returns_dict(tmp_dir):
    p = _write(tmp_dir, "empty.json", "{}")
    result = load_file(p)
    assert result == {}


def test_yaml_null_values(tmp_dir):
    p = _write(tmp_dir, "nulls.yaml", "key: null\nother: ~\n")
    result = load_file(p)
    assert result["key"] is None
    assert result["other"] is None


def test_yaml_list_at_root(tmp_dir):
    p = _write(tmp_dir, "list.yaml", "- a\n- b\n- c\n")
    result = load_file(p)
    assert result == ["a", "b", "c"]
