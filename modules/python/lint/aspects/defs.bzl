# Public aspects of this module. They are loaded under private aliases and
# re-bound to public names: a bare `load()` symbol is NOT exported for the
# `defs.bzl%<aspect>` reference, but a top-level assignment is. The aspect names
# are the stable entry points consumers put in their .bazelrc.
load(
    ":python.bzl",
    _python_archtest_submission_aspect = "python_archtest_submission_aspect",
    _python_bandit_submission_aspect = "python_bandit_submission_aspect",
    _python_pycompile_submission_aspect = "python_pycompile_submission_aspect",
    _python_ruff_submission_aspect = "python_ruff_submission_aspect",
)

python_pycompile_submission_aspect = _python_pycompile_submission_aspect

python_ruff_submission_aspect = _python_ruff_submission_aspect

python_bandit_submission_aspect = _python_bandit_submission_aspect

python_archtest_submission_aspect = _python_archtest_submission_aspect
