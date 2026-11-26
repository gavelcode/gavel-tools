"""Release stamp for the prebuilt gavel-lint binaries.

This committed copy is a placeholder: development builds gavel-lint from source.
`.github/ci/release_prep.sh` replaces it inside the release archive only, with the
tag's version and each platform binary's sha256.
"""

GAVEL_LINT_VERSION = "0.0.0"

# platform ("<os>_<arch>") -> sha256 hex of its gavel-lint binary.
GAVEL_LINT_INTEGRITY = {}
