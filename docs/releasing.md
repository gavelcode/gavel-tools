---
title: Releasing
type: how-to
description: One command tags a version and publishes it to the Bazel Central Registry.
resource: https://github.com/gavelcode/gavel-tools/blob/main/.github/ci/release.sh
tags: [release, versioning, registry]
---

# Releasing

A release is one command, from `main`:

```bash
bash .github/ci/release.sh X.Y.Z
```

It bumps the version of every module (the root `MODULE.bazel` and each
`modules/<lang>/MODULE.bazel`, which share one version), pushes it, waits for CI to pass on that
exact commit, then tags `vX.Y.Z`. The tag drives
[`publish.yaml`](../.github/workflows/publish.yaml), which uploads a stable
release-asset tarball and opens one pull request adding every module at `X.Y.Z` —
`gavel_tools` and each `gavel_tools_<lang>` — to the
[Bazel Central Registry](https://registry.bazel.build/modules/gavel_tools). The module
roots and their per-module templates are in [`.bcr/`](../.bcr/config.yml) (publish-to-bcr
`moduleRoots`; each module's `source.template.json` strips the archive down to its own
directory, and its `presubmit.yml` builds what exists in the archive).

You then approve that PR yourself — you are a listed module maintainer, so no BCR
maintainer review is needed once the first version is in and the `presubmit.yml`
is unchanged. (That holds per module: the first release of each *new* `gavel_tools_<lang>`
module goes through normal BCR maintainer review.)

## What a release contains

Besides the source archive, a release carries the prebuilt `gavel-lint-<os>_<arch>`
helper binaries, so consumers never compile Go for gavel. Both are produced by
[`release_prep.sh`](../.github/ci/release_prep.sh) (read its header for what it
stamps and why; it also checks that every module is at the tag's version), which [`publish.yaml`](../.github/workflows/publish.yaml) runs.
Run it yourself to rehearse a release: the stamp is applied to the archive only,
never to git. To try a rehearsal against a consumer, point
`--repo_env=GAVEL_LINT_BASE_URL` at the directory holding the binaries
(`file://…` works) and `local_path_override` at the extracted archive and at each
`modules/<lang>` inside it.

## One-time setup

Publishing opens a PR against the BCR fork, so it needs a `PUBLISH_TO_BCR_TOKEN`
secret in this repo — a classic PAT that can push to `gavelcode/bazel-central-registry`
and open pull requests. Set it under *Settings → Secrets and variables → Actions*.

## Why the tag drives everything

The version lives in the `MODULE.bazel` files — all bumped together by the release script,
which is the only thing that edits them (`//lint:lint_test` fails if they ever differ) — so the tag and the module version can never
disagree. The registry entry is generated from the tag, never written by hand.

## The gavel registry is frozen

Earlier versions were also mirrored to a separate [gavel registry](https://github.com/gavelcode/registry)
(versions `0.1.0`–`0.3.10`). Now that gavel_tools is in the Bazel Central
Registry — the default registry every Bazel user already reads — that mirror is
frozen: no new versions are published to it. Consumers need no custom `--registry`
line; a plain `bazel_dep` resolves against BCR.
