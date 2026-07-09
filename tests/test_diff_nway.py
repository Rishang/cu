"""Tests for N-way comparison (3+ files → all N-choose-2 pairs)."""

from itertools import combinations


from cloudutil.diff.engine import compute_diff
from cloudutil.diff.schemas import Diff, DiffConfig


# ── helpers ────────────────────────────────────────────────────────────────────


def _pairs_count(n: int) -> int:
    return n * (n - 1) // 2


# ── N-way combination logic ────────────────────────────────────────────────────


def test_two_files_one_pair():
    files = ["a.yaml", "b.yaml"]
    pairs = list(combinations(files, 2))
    assert len(pairs) == 1
    assert pairs[0] == ("a.yaml", "b.yaml")


def test_three_files_three_pairs():
    files = ["a.yaml", "b.yaml", "c.yaml"]
    pairs = list(combinations(files, 2))
    assert len(pairs) == 3
    assert set(pairs) == {
        ("a.yaml", "b.yaml"),
        ("a.yaml", "c.yaml"),
        ("b.yaml", "c.yaml"),
    }


def test_four_files_six_pairs():
    files = ["a.yaml", "b.yaml", "c.yaml", "d.yaml"]
    pairs = list(combinations(files, 2))
    assert len(pairs) == 6


def test_diff_model_accepts_three_files():
    """DiffConfig model should accept entries with 3+ files."""
    entry = Diff(files=["a.yaml", "b.yaml", "c.yaml"])
    cfg = DiffConfig(diffs=[entry])
    assert len(cfg.diffs[0].files) == 3


def test_nway_diffs_detect_all_differences():
    """3-way diff should find differences across all pairs."""
    a = {"env": "dev", "replicas": 1}
    b = {"env": "stage", "replicas": 2}
    c = {"env": "prod", "replicas": 5}

    ab = compute_diff(a, b)
    ac = compute_diff(a, c)
    bc = compute_diff(b, c)

    assert len(ab) == 2
    assert len(ac) == 2
    assert len(bc) == 2

    # A↔C should find replicas: 1→5
    replicas_diff = next(e for e in ac if e.path_str == "replicas")
    assert replicas_diff.old_value == 1
    assert replicas_diff.new_value == 5


def test_nway_identical_pair_no_diffs():
    """Two identical files in a 3-way set should produce no diffs for that pair."""
    a = {"version": "1.0"}
    b = {"version": "1.0"}
    c = {"version": "2.0"}
    assert compute_diff(a, b) == []
    assert len(compute_diff(a, c)) == 1
