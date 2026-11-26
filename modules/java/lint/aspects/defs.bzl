# Public aspects of this module. They are loaded under private aliases and
# re-bound to public names: a bare `load()` symbol is NOT exported for the
# `defs.bzl%<aspect>` reference, but a top-level assignment is. The aspect names
# are the stable entry points consumers put in their .bazelrc.
load(
    ":java.bzl",
    _java_archtest_submission_aspect = "java_archtest_submission_aspect",
    _java_cpd_submission_aspect = "java_cpd_submission_aspect",
    _java_error_prone_submission_aspect = "java_error_prone_submission_aspect",
    _java_pmd_submission_aspect = "java_pmd_submission_aspect",
    _java_spotbugs_submission_aspect = "java_spotbugs_submission_aspect",
)

java_pmd_submission_aspect = _java_pmd_submission_aspect

java_cpd_submission_aspect = _java_cpd_submission_aspect

java_spotbugs_submission_aspect = _java_spotbugs_submission_aspect

java_error_prone_submission_aspect = _java_error_prone_submission_aspect

java_archtest_submission_aspect = _java_archtest_submission_aspect
