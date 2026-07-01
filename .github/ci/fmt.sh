#!/usr/bin/env bash
# Apply every auto-fix the gates check for, so `check.sh` goes green: format Go
# and Starlark, regenerate BUILD files, tidy the module, and ratchet the coverage
# baseline up to the current measurement. Safe to run repeatedly; it only writes
# what the corresponding gate would otherwise flag.

set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

cd "${REPO_ROOT}" || exit 1

info "gofmt -w"
gofmt -w lint
pass "Go formatted"

info "buildifier -mode=fix"
build_files | xargs go run "${BUILDIFIER_PKG}" -mode=fix
pass "Starlark formatted"

info "gazelle"
bazel run //:gazelle >/dev/null 2>&1
pass "BUILD files regenerated"

info "go mod tidy"
go mod tidy
pass "module tidied"

info "coverage baseline"
if run_coverage && pct="$(coverage_percent)"; then
  baseline="$(read_baseline)"
  if ge "${pct}" "${baseline}" && ! ge "${baseline}" "${pct}"; then
    printf '%s\n' "${pct}" >"${COVERAGE_BASELINE_FILE}"
    pass "baseline ratcheted ${baseline}% → ${pct}%"
  else
    pass "baseline unchanged at ${baseline}% (current ${pct}%)"
  fi
else
  warn "could not measure coverage; baseline left untouched"
fi

info "done — run .github/ci/check.sh to verify"
