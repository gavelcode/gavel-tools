# Per-language aspects live in <lang>.bzl. They are loaded under private aliases
# and re-bound to public names below: a bare `load()` symbol is NOT exported for
# the `defs.bzl%<aspect>` reference, but a top-level assignment is. This keeps
# the public entry points (in every consumer's .bazelrc) stable while each
# language's implementation lives in its own file.
load(
    ":go.bzl",
    _go_archtest_submission_aspect = "go_archtest_submission_aspect",
    _go_golangci_lint_submission_aspect = "go_golangci_lint_submission_aspect",
)
load(
    ":java.bzl",
    _java_archtest_submission_aspect = "java_archtest_submission_aspect",
    _java_cpd_submission_aspect = "java_cpd_submission_aspect",
    _java_error_prone_submission_aspect = "java_error_prone_submission_aspect",
    _java_pmd_submission_aspect = "java_pmd_submission_aspect",
    _java_spotbugs_submission_aspect = "java_spotbugs_submission_aspect",
)
load(
    ":python.bzl",
    _python_archtest_submission_aspect = "python_archtest_submission_aspect",
    _python_bandit_submission_aspect = "python_bandit_submission_aspect",
    _python_pycompile_submission_aspect = "python_pycompile_submission_aspect",
    _python_ruff_submission_aspect = "python_ruff_submission_aspect",
)
load(
    ":rust.bzl",
    _rust_archtest_submission_aspect = "rust_archtest_submission_aspect",
    _rust_clippy_submission_aspect = "rust_clippy_submission_aspect",
)
load(
    ":typescript.bzl",
    _typescript_archtest_submission_aspect = "typescript_archtest_submission_aspect",
    _typescript_eslint_submission_aspect = "typescript_eslint_submission_aspect",
)

go_golangci_lint_submission_aspect = _go_golangci_lint_submission_aspect

go_archtest_submission_aspect = _go_archtest_submission_aspect

java_pmd_submission_aspect = _java_pmd_submission_aspect

java_cpd_submission_aspect = _java_cpd_submission_aspect

java_spotbugs_submission_aspect = _java_spotbugs_submission_aspect

java_error_prone_submission_aspect = _java_error_prone_submission_aspect

java_archtest_submission_aspect = _java_archtest_submission_aspect

python_pycompile_submission_aspect = _python_pycompile_submission_aspect

python_ruff_submission_aspect = _python_ruff_submission_aspect

python_bandit_submission_aspect = _python_bandit_submission_aspect

python_archtest_submission_aspect = _python_archtest_submission_aspect

rust_clippy_submission_aspect = _rust_clippy_submission_aspect

rust_archtest_submission_aspect = _rust_archtest_submission_aspect

typescript_eslint_submission_aspect = _typescript_eslint_submission_aspect

typescript_archtest_submission_aspect = _typescript_archtest_submission_aspect
