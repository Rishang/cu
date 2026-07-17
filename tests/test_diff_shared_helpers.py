"""Regression tests for helpers shared by modern diff and legacy ydiff."""

from cloudutil.diff.patterns import (
    any_pattern_matches,
    compile_patterns,
    values_similar_after_stripping,
)


def test_modern_diff_pattern_semantics_are_case_insensitive_and_split_commas():
    compiled = compile_patterns([" dev, PROD "], split_commas=True, ignore_case=True)

    assert any_pattern_matches(compiled, "DEV-api")
    assert any_pattern_matches(compiled, "prod-api")
    assert not any_pattern_matches(compiled, "mydevapi")


def test_legacy_ydiff_pattern_semantics_remain_case_sensitive():
    compiled = compile_patterns(["dev"])

    assert any_pattern_matches(compiled, "dev-api")
    assert not any_pattern_matches(compiled, "DEV-api")


def test_none_stringification_can_preserve_legacy_ydiff_behavior():
    compiled = compile_patterns(["None"])

    assert values_similar_after_stripping(
        compiled, None, None, threshold=1.0, none_as_empty=False
    )
    assert not values_similar_after_stripping(
        compiled, None, "different", threshold=1.0, none_as_empty=False
    )
