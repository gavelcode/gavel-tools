# Public aspects of this module. They are loaded under private aliases and
# re-bound to public names: a bare `load()` symbol is NOT exported for the
# `defs.bzl%<aspect>` reference, but a top-level assignment is. The aspect names
# are the stable entry points consumers put in their .bazelrc.
load(
    ":typescript.bzl",
    _typescript_archtest_submission_aspect = "typescript_archtest_submission_aspect",
    _typescript_eslint_submission_aspect = "typescript_eslint_submission_aspect",
)

typescript_eslint_submission_aspect = _typescript_eslint_submission_aspect

typescript_archtest_submission_aspect = _typescript_archtest_submission_aspect
