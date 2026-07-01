#!/usr/bin/env bash
# Shared vocabulary for the gavel-tools CI gates. Sourced by check.sh (verify,
# read-only) and fmt.sh (apply the auto-fixes). It owns no gate logic itself —
# only the paths, pinned tool versions, and helpers both scripts agree on.

# lib.sh is a sourced constants+helpers file; the pins and paths it defines are
# consumed by check.sh and fmt.sh, not here.
# shellcheck disable=SC2034
set -euo pipefail

CI_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${CI_DIR}/../.." && pwd)"

# Pinned `go run` tools — reproducible, no host installs, no floating @latest.
BUILDIFIER_PKG="github.com/bazelbuild/buildtools/buildifier@v0.0.0-20260622120422-77b9b380c0a4"
DEADCODE_PKG="golang.org/x/tools/cmd/deadcode@v0.47.0"
GOVULNCHECK_PKG="golang.org/x/vuln/cmd/govulncheck@v1.5.0"

# Absolute line-coverage minimum. The ratchet baseline may only sit at or above
# it; it never drops below.
COVERAGE_FLOOR="95.0"
COVERAGE_BASELINE_FILE="${CI_DIR}/coverage-baseline"

# The combined lcov report bazel coverage writes for the whole repo.
COVERAGE_REPORT="${REPO_ROOT}/bazel-out/_coverage/_coverage_report.dat"

if [ -t 1 ]; then
  C_RED=$'\033[31m'
  C_GREEN=$'\033[32m'
  C_YELLOW=$'\033[33m'
  C_BOLD=$'\033[1m'
  C_OFF=$'\033[0m'
else
  C_RED='' C_GREEN='' C_YELLOW='' C_BOLD='' C_OFF=''
fi

info() { printf '\n%s→ %s%s\n' "$C_BOLD" "$*" "$C_OFF"; }
pass() { printf '%s✓%s %s\n' "$C_GREEN" "$C_OFF" "$*"; }
warn() { printf '%s!%s %s\n' "$C_YELLOW" "$C_OFF" "$*"; }
fail() { printf '%s✗%s %s\n' "$C_RED" "$C_OFF" "$*"; }

# build_files lists every Starlark file gazelle/buildifier own, skipping the
# bazel-* convenience symlinks so we never descend into the output tree.
build_files() {
  find "${REPO_ROOT}" -path "${REPO_ROOT}/bazel-*" -prune -o \
    \( -name BUILD.bazel -o -name BUILD -o -name '*.bzl' -o -name MODULE.bazel -o -name WORKSPACE \) \
    -print
}

# run_coverage produces the combined lcov report; separated from parsing so
# check.sh and fmt.sh share one measurement path.
run_coverage() {
  ( cd "${REPO_ROOT}" && bazel coverage //... --combined_report=lcov >/dev/null 2>&1 )
}

# coverage_percent sums LF/LH across the combined report and prints the line
# coverage as a two-decimal percentage.
coverage_percent() {
  [ -f "${COVERAGE_REPORT}" ] || return 1
  awk -F: '/^LF:/{lf+=$2} /^LH:/{lh+=$2} END{if(lf>0) printf "%.2f", lh/lf*100; else print "0.00"}' \
    "${COVERAGE_REPORT}"
}

# ge compares two decimal percentages: succeeds when $1 >= $2.
ge() { awk -v a="$1" -v b="$2" 'BEGIN{exit !(a+0 >= b+0)}'; }

read_baseline() {
  if [ -f "${COVERAGE_BASELINE_FILE}" ]; then
    tr -d '[:space:]' <"${COVERAGE_BASELINE_FILE}"
  else
    printf '%s' "${COVERAGE_FLOOR}"
  fi
}
