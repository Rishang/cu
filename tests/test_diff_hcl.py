"""Tests for HCL (.tf / .hcl / .tfvars) support in cu diff."""

import textwrap
from pathlib import Path

import pytest

from cloudutil.diff.engine import compute_diff
from cloudutil.diff.loader import load_file
from cloudutil.diff.normalize import normalize
from cloudutil.diff.renderer import _fmt_hcl


# ── fixtures ──────────────────────────────────────────────────────────────────

PROD_TFVARS = Path(__file__).parent / "assets" / "prod.tfvars"
STAGE_TFVARS = Path(__file__).parent / "assets" / "stage.tfvars"
INFRA_A = Path(__file__).parent / "assets" / "infra-a.tf"
INFRA_B = Path(__file__).parent / "assets" / "infra-b.tf"


@pytest.fixture()
def tmp_dir(tmp_path: Path) -> Path:
    return tmp_path


def _tf(tmp_dir: Path, name: str, content: str) -> Path:
    p = tmp_dir / name
    p.write_text(textwrap.dedent(content), encoding="utf-8")
    return p


# ── loader — .tfvars ──────────────────────────────────────────────────────────


def test_load_tfvars_returns_dict():
    data = load_file(PROD_TFVARS)
    assert isinstance(data, dict)


def test_load_tfvars_keys_present():
    data = load_file(PROD_TFVARS)
    assert "region" in data
    assert "instance_type" in data
    assert "replica_count" in data
    assert "enable_dns" in data
    assert "tags" in data


def test_load_tfvars_string_value():
    # hcl2 preserves HCL string literals with embedded double-quote chars
    data = load_file(PROD_TFVARS)
    assert data["region"] == '"us-east-1"'


def test_load_tfvars_number_value():
    data = load_file(PROD_TFVARS)
    assert data["replica_count"] == 2


def test_load_tfvars_bool_value():
    data = load_file(PROD_TFVARS)
    assert data["enable_dns"] is True


def test_load_tfvars_map_value():
    data = load_file(PROD_TFVARS)
    assert isinstance(data["tags"], dict)
    assert data["tags"]["env"] == '"prod"'


def test_load_hcl_extension(tmp_dir):
    # .hcl files use the same parser
    p = _tf(tmp_dir, "config.hcl", 'key = "value"\n')
    result = load_file(p)
    assert result == {"key": '"value"'}


def test_load_tfvars_invalid_raises(tmp_dir):
    p = _tf(tmp_dir, "bad.tfvars", "this is not { valid hcl !!!")
    with pytest.raises(ValueError, match="Invalid HCL"):
        load_file(p)


# ── loader — .tf blocks ───────────────────────────────────────────────────────


def test_load_tf_returns_dict():
    data = load_file(INFRA_A)
    assert isinstance(data, dict)


def test_load_tf_top_level_keys():
    data = load_file(INFRA_A)
    assert "variable" in data
    assert "resource" in data


def test_load_tf_block_labels_have_embedded_quotes():
    # hcl2 stores block labels as strings with embedded quote chars
    data = load_file(INFRA_A)
    keys = list(data["variable"][0].keys())
    assert any(k == '"region"' for k in keys)


def test_load_tf_block_default_value():
    data = load_file(INFRA_A)
    region_block = data["variable"][0]['"region"']
    assert region_block["default"] == '"us-east-1"'


# ── diff — .tfvars ────────────────────────────────────────────────────────────


def test_diff_tfvars_detects_changes():
    a = load_file(PROD_TFVARS)
    b = load_file(STAGE_TFVARS)
    entries = compute_diff(normalize(a), normalize(b))
    assert len(entries) > 0


def test_diff_tfvars_region_changed():
    a = load_file(PROD_TFVARS)
    b = load_file(STAGE_TFVARS)
    entries = compute_diff(normalize(a), normalize(b))
    changed = {e.path_str for e in entries if e.kind == "changed"}
    assert "region" in changed


def test_diff_tfvars_bool_changed():
    a = load_file(PROD_TFVARS)
    b = load_file(STAGE_TFVARS)
    entries = compute_diff(normalize(a), normalize(b))
    changed = {e.path_str for e in entries if e.kind == "changed"}
    assert "enable_dns" in changed


def test_diff_tfvars_no_false_positives(tmp_dir):
    content = 'region = "us-east-1"\ncount = 2\n'
    fa = _tf(tmp_dir, "a.tfvars", content)
    fb = _tf(tmp_dir, "b.tfvars", content)
    entries = compute_diff(normalize(load_file(fa)), normalize(load_file(fb)))
    assert entries == []


def test_diff_tfvars_new_key_detected(tmp_dir):
    fa = _tf(tmp_dir, "a.tfvars", 'region = "us-east-1"\n')
    fb = _tf(tmp_dir, "b.tfvars", 'region = "us-east-1"\nextra = "new"\n')
    entries = compute_diff(normalize(load_file(fa)), normalize(load_file(fb)))
    assert any(e.kind == "added" and e.path_str == "extra" for e in entries)


# ── _fmt_hcl — value formatter ────────────────────────────────────────────────


@pytest.mark.parametrize(
    "value,expected",
    [
        (None, "null"),
        (True, "true"),
        (False, "false"),
        (42, "42"),
        (3.14, "3.14"),
        # bare strings (no embedded hcl2 quotes) get wrapped
        ("hello", '"hello"'),
        # hcl2-style pre-quoted strings pass through unchanged
        ('"us-east-1"', '"us-east-1"'),
        ('"t3.small"', '"t3.small"'),
        ([1, 2], "[1, 2]"),
        ({}, "{}"),
        ({"k": "v"}, '{ k = "v" }'),
    ],
)
def test_fmt_hcl(value, expected):
    assert _fmt_hcl(value) == expected


def test_fmt_hcl_bool_before_int():
    # bool is a subclass of int — must not render True as '1'
    assert _fmt_hcl(True) == "true"
    assert _fmt_hcl(1) == "1"


def test_fmt_hcl_skips_is_block():
    # __is_block__ is hcl2 metadata, should not appear in formatted output
    val = {"ami": '"ami-123"', "__is_block__": True}
    result = _fmt_hcl(val)
    assert "__is_block__" not in result
    assert '"ami-123"' in result


def test_fmt_hcl_hcl2_string_passthrough():
    # real hcl2 parsed string from a tfvars file
    assert _fmt_hcl('"us-east-1"') == '"us-east-1"'


def test_fmt_hcl_nested_map():
    # tags = { env = "prod", team = "infra" }
    val = {"env": '"prod"', "team": '"infra"'}
    result = _fmt_hcl(val)
    assert 'env = "prod"' in result
    assert 'team = "infra"' in result
