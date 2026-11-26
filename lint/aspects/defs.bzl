# Aspects of the core module: only those that load no language ruleset, so they
# work for any consumer. Language-specific aspects are published by the
# per-language modules (see docs/repository-layout.md). Loaded under a private
# alias and re-bound: a bare `load()` symbol is NOT exported for the
# `defs.bzl%<aspect>` reference, but a top-level assignment is.
load(":rust.bzl", _rust_archtest_submission_aspect = "rust_archtest_submission_aspect")

rust_archtest_submission_aspect = _rust_archtest_submission_aspect
