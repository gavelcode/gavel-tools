#!/usr/bin/env bash
# The gavel-tools quality gate. Runs every verification the CI enforces, in one
# place, so `bash .github/ci/check.sh` locally is exactly what CI runs. Gates are
# read-only; the two that must mutate to check (gazelle, go mod tidy) restore the
# tree before returning. Exit is non-zero if any gate fails; all gates run so you
# see the full picture, not just the first failure.

set -uo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

cd "${REPO_ROOT}" || exit 1

FAILURES=()
record() { FAILURES+=("$1"); }

gate_tests() {
  info "tests — bazel test //..."
  if bazel test //... 2>&1 | tail -3; then
    pass "tests: all pass"
  else
    fail "tests: failures above"
    record "tests"
  fi
}

gate_coverage() {
  local baseline
  baseline="$(read_baseline)"
  info "coverage — floor ${COVERAGE_FLOOR}%, ratchet ${baseline}%"
  if ! run_coverage; then
    fail "coverage: bazel coverage failed"
    record "coverage"
    return
  fi
  local pct
  if ! pct="$(coverage_percent)"; then
    fail "coverage: no combined report at ${COVERAGE_REPORT}"
    record "coverage"
    return
  fi
  if ! ge "${pct}" "${COVERAGE_FLOOR}"; then
    fail "coverage: ${pct}% below floor ${COVERAGE_FLOOR}%"
    record "coverage"
    return
  fi
  if ! ge "${pct}" "${baseline}"; then
    fail "coverage: ${pct}% below ratchet baseline ${baseline}% — a regression"
    record "coverage"
    return
  fi
  if ge "${pct}" "$(awk -v b="${baseline}" 'BEGIN{printf "%.2f", b+0.01}')"; then
    pass "coverage: ${pct}% (above baseline ${baseline}% — run fmt.sh to ratchet up)"
  else
    pass "coverage: ${pct}%"
  fi
}

gate_gofmt() {
  info "gofmt"
  local unformatted
  unformatted="$(gofmt -l lint 2>/dev/null)"
  if [ -n "${unformatted}" ]; then
    fail "gofmt: files need formatting"
    printf '%s\n' "${unformatted}"
    record "gofmt"
  else
    pass "gofmt: clean"
  fi
}

gate_buildifier() {
  info "buildifier"
  local out
  if out="$(build_files | xargs go run "${BUILDIFIER_PKG}" -mode=check 2>&1)"; then
    pass "buildifier: clean"
  else
    fail "buildifier: files need formatting"
    printf '%s\n' "${out}"
    record "buildifier"
  fi
}

gate_gazelle() {
  info "gazelle — BUILD files up to date"
  if ! bazel run //:gazelle >/dev/null 2>&1; then
    fail "gazelle: run failed"
    record "gazelle"
    return
  fi
  local changed
  changed="$(git diff --name-only -- ':(glob)**/BUILD.bazel' 'BUILD.bazel' 2>/dev/null)"
  if [ -n "${changed}" ]; then
    fail "gazelle: BUILD files are stale — run .github/ci/fmt.sh"
    printf '%s\n' "${changed}"
    # shellcheck disable=SC2086
    git checkout -- ${changed} 2>/dev/null || true
    record "gazelle"
  else
    pass "gazelle: up to date"
  fi
}

gate_selflint() {
  info "self-lint — golangci aspect (dogfood), fail on findings"
  # export_stdlib emits the compiled stdlib .a archives that the driver wires as
  # export data; without it golangci-lint cannot type-check and only emits noise.
  if ! bazel build //... \
    --@rules_go//go/config:export_stdlib=True \
    --aspects=//lint/aspects:defs.bzl%go_golangci_lint_submission_aspect \
    --output_groups=gavel_submissions >/dev/null 2>&1; then
    fail "self-lint: aspect build failed"
    record "self-lint"
    return
  fi
  local total=0 count report
  while IFS= read -r report; do
    count="$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(sum(len(r.get("results",[])) for r in d.get("runs",[])))' "${report}" 2>/dev/null || echo 0)"
    if [ "${count}" -gt 0 ]; then
      fail "  ${count} finding(s): ${report#"${REPO_ROOT}"/}"
      total=$((total + count))
    fi
  done < <(find -L "${REPO_ROOT}/bazel-out" -name '*.golangci.sarif' 2>/dev/null)
  if [ "${total}" -gt 0 ]; then
    fail "self-lint: ${total} finding(s)"
    record "self-lint"
  else
    pass "self-lint: 0 findings"
  fi
}

gate_deadcode() {
  info "deadcode — unreachable code"
  local out
  out="$(go run "${DEADCODE_PKG}" ./... 2>/dev/null)"
  if [ -n "${out}" ]; then
    fail "deadcode: unreachable functions"
    printf '%s\n' "${out}"
    record "deadcode"
  else
    pass "deadcode: none"
  fi
}

gate_govulncheck() {
  info "govulncheck — known vulnerabilities"
  local out
  if out="$(go run "${GOVULNCHECK_PKG}" ./... 2>&1)"; then
    pass "govulncheck: no vulnerabilities"
  else
    fail "govulncheck: vulnerabilities found"
    printf '%s\n' "${out}" | tail -30
    record "govulncheck"
  fi
}

gate_gomodtidy() {
  info "go mod tidy — go.mod/go.sum minimal"
  local tmp
  tmp="$(mktemp -d)"
  cp go.mod go.sum "${tmp}/"
  go mod tidy >/dev/null 2>&1
  local dirty=0
  diff -q go.mod "${tmp}/go.mod" >/dev/null 2>&1 || dirty=1
  diff -q go.sum "${tmp}/go.sum" >/dev/null 2>&1 || dirty=1
  cp "${tmp}/go.mod" go.mod
  cp "${tmp}/go.sum" go.sum
  rm -rf "${tmp}"
  if [ "${dirty}" = 1 ]; then
    fail "go mod tidy: go.mod/go.sum not tidy — run go mod tidy and commit"
    record "go mod tidy"
  else
    pass "go mod tidy: clean"
  fi
}

gate_shellcheck() {
  info "shellcheck — CI scripts"
  if ! command -v shellcheck >/dev/null 2>&1; then
    warn "shellcheck: not installed, skipping (CI installs it)"
    return
  fi
  if shellcheck -x "${CI_DIR}"/*.sh; then
    pass "shellcheck: clean"
  else
    fail "shellcheck: issues above"
    record "shellcheck"
  fi
}

main() {
  if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
    warn "working tree is dirty — format/tidy gates report your uncommitted changes too"
  fi

  gate_tests
  gate_coverage
  gate_gofmt
  gate_buildifier
  gate_gazelle
  gate_selflint
  gate_deadcode
  gate_govulncheck
  gate_gomodtidy
  gate_shellcheck

  printf '\n%s──────── summary ────────%s\n' "$C_BOLD" "$C_OFF"
  if [ "${#FAILURES[@]}" -eq 0 ]; then
    pass "all gates green"
    return 0
  fi
  fail "${#FAILURES[@]} gate(s) failed: ${FAILURES[*]}"
  return 1
}

main "$@"
