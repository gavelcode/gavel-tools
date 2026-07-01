#!/usr/bin/env bash
# One-command release for the gavel_tools Bazel module.
#
#   bash .github/ci/release.sh 0.2.0
#
# Bumps the module version, pushes it, waits for CI to go green on that exact
# commit, then tags. The tag triggers .github/workflows/release.yml, which
# publishes the version to the Bazel registry (gavelcode/registry). The only
# irreversible public actions — pushing the commit and the tag — happen last,
# after every check passes and you confirm.
#
# RELEASE_YES=1 skips the confirmation prompt (for automation).

set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

cd "${REPO_ROOT}" || exit 1

REPO="gavelcode/gavel-tools"
CI_WORKFLOW="ci.yml"
CI_POLL_SECONDS=20
CI_POLL_ATTEMPTS=90

die() {
  fail "release: $*"
  exit 1
}

VERSION="${1:-}"
[ -n "${VERSION}" ] || die "usage: bash .github/ci/release.sh X.Y.Z"
echo "${VERSION}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' ||
  die "VERSION must be semver X.Y.Z (got '${VERSION}')"
TAG="v${VERSION}"

command -v git >/dev/null || die "git is required"
command -v gh >/dev/null || die "gh (GitHub CLI) is required — brew install gh"

branch="$(git rev-parse --abbrev-ref HEAD)"
[ "${branch}" = "main" ] || die "must release from 'main' (on '${branch}')"

if ! git diff --quiet || ! git diff --cached --quiet; then
  die "working tree is dirty — commit or stash first"
fi

git fetch --quiet origin main
[ "$(git rev-parse @)" = "$(git rev-parse '@{u}')" ] ||
  die "local main is not in sync with origin/main — push or pull first"

git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null 2>&1 &&
  die "tag ${TAG} already exists locally"
git ls-remote --exit-code --tags origin "${TAG}" >/dev/null 2>&1 &&
  die "tag ${TAG} already exists on origin"

# Bump the module() version — the first `version = "..."` in MODULE.bazel, ahead
# of the bazel_dep and tool-binary versions.
current="$(grep -m1 -E '^\s*version = "' MODULE.bazel | sed -E 's/.*"([^"]+)".*/\1/')"
[ -n "${current}" ] || die "could not read the current version from MODULE.bazel"
if [ "${current}" = "${VERSION}" ]; then
  die "MODULE.bazel is already at ${VERSION}"
fi
info "release: bumping MODULE.bazel ${current} -> ${VERSION}"
perl -pi -e 'if (!$seen && s/version = "[^"]+"/version = "'"${VERSION}"'"/) { $seen = 1 }' MODULE.bazel
git add MODULE.bazel
git commit -q -m "chore(release): v${VERSION}"
git push -q origin main
sha="$(git rev-parse @)"
pass "release: pushed the version bump (${sha})"

info "release: waiting for CI (${CI_WORKFLOW}) to pass on ${sha} ..."
attempt=0
while :; do
  attempt=$((attempt + 1))
  status="$(gh run list -R "${REPO}" --workflow "${CI_WORKFLOW}" --commit "${sha}" \
    --json status,conclusion --jq '.[0] | "\(.status)/\(.conclusion)"' 2>/dev/null || true)"
  case "${status}" in
    completed/success) pass "release: CI is green"; break ;;
    completed/*) die "CI for ${sha} finished '${status}' — fix it, then release again" ;;
  esac
  [ "${attempt}" -ge "${CI_POLL_ATTEMPTS}" ] && die "timed out waiting for CI on ${sha}"
  sleep "${CI_POLL_SECONDS}"
done

echo
info "release: ready to tag ${TAG} at ${sha} — this publishes ${VERSION} to the registry"
if [ "${RELEASE_YES:-}" != "1" ]; then
  printf "release: push %s and publish? [y/N] " "${TAG}"
  read -r reply
  case "${reply}" in
    y | Y) ;;
    *) die "aborted (the version bump is already on main; re-run to tag it)" ;;
  esac
fi

git tag -a "${TAG}" -m "Release ${TAG}"
git push -q origin "${TAG}"
pass "release: pushed ${TAG}"
info "release: watch the publish with  gh run watch -R ${REPO} --workflow release.yml"
