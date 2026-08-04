"""Regression tests for the diff command's ignore-pattern helpers."""

from cloudutil.diff.patterns import compile_patterns, values_similar_after_stripping


def test_patterns_split_commas_and_ignore_case():
    compiled = compile_patterns([" dev, PROD "])

    assert values_similar_after_stripping(
        compiled,
        "DEV-api",
        "prod-api",
        threshold=1.0,  # gitleaks:allow - hostnames
    )
    assert not values_similar_after_stripping(
        compiled,
        "mydevapi",
        "myprodapi",
        threshold=1.0,  # gitleaks:allow - hostnames
    )


def test_absent_values_compare_as_empty():
    compiled = compile_patterns(["dev"])

    assert values_similar_after_stripping(compiled, None, "", threshold=1.0)
    assert not values_similar_after_stripping(
        compiled, None, "different", threshold=1.0
    )
