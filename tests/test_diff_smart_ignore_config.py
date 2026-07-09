"""Integration tests: smart ignore + N-way via config YAML."""

from pathlib import Path

from cloudutil.diff.engine import compute_diff
from cloudutil.diff.filters import apply_filters

ASSETS = Path(__file__).parent / "assets"


def _load_yaml(name: str) -> dict:
    import yaml

    return yaml.safe_load((ASSETS / name).read_text()) or {}


# ── smart ignore integration ───────────────────────────────────────────────────


def test_env_hostnames_ignored_with_smart_patterns():
    """dev-api.example.com vs prod-api.example.com should be ignored with patterns ['dev','prod']."""
    dev = _load_yaml("app-dev.yaml")
    prod = _load_yaml("app-prod.yaml")
    entries = compute_diff(dev, prod)
    kept, ignored = apply_filters(
        entries, global_ignore_patterns=["dev", "stage", "prod"]
    )
    ignored_paths = {e.path_str for e in ignored}
    kept_paths = {e.path_str for e in kept}
    # env-qualified hostnames should be suppressed
    assert "app.host" in ignored_paths
    assert "database.host" in ignored_paths
    # structural differences (replicas, pool_size) should pass through
    assert "app.replicas" in kept_paths
    assert "database.pool_size" in kept_paths


def test_nway_config_model_parses():
    """cu_diff.yml should parse into DiffConfig with 1 entry holding 3 files."""
    import yaml
    from cloudutil.diff.schemas import DiffConfig

    raw = yaml.safe_load((ASSETS / "cu_diff.yml").read_text()) or {}
    cfg = DiffConfig(**raw)
    assert len(cfg.diffs) == 1
    assert len(cfg.diffs[0].files) == 3
    assert cfg.format == "table"
    assert "dev" in cfg.global_ignore_patterns


def test_nway_three_envs_all_pairs_have_diffs():
    """Each pair in the 3-way comparison should find differences."""
    from itertools import combinations

    envs = ["app-dev.yaml", "app-stage.yaml", "app-prod.yaml"]
    all_diffs = {}
    for fa, fb in combinations(envs, 2):
        a = _load_yaml(fa)
        b = _load_yaml(fb)
        all_diffs[(fa, fb)] = compute_diff(a, b)
    assert all(len(d) > 0 for d in all_diffs.values())


def test_nway_smart_ignore_reduces_noise():
    """After applying smart ignore patterns, only structural diffs remain."""
    from itertools import combinations

    envs = ["app-dev.yaml", "app-stage.yaml", "app-prod.yaml"]
    for fa, fb in combinations(envs, 2):
        a = _load_yaml(fa)
        b = _load_yaml(fb)
        entries = compute_diff(a, b)
        kept, ignored = apply_filters(
            entries, global_ignore_patterns=["dev", "stage", "prod"]
        )
        # After filtering, should only see structural (non-env-name) differences
        for e in kept:
            assert "host" not in e.path_str, (
                f"Expected host to be ignored, got {e.path_str}"
            )
