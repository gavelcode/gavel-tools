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

It bumps the `MODULE.bazel` version, pushes it, waits for CI to pass on that
exact commit, then tags `vX.Y.Z`. The tag drives
[`publish.yaml`](../.github/workflows/publish.yaml), which uploads a stable
release-asset tarball and opens a pull request adding `gavel_tools@X.Y.Z` to the
[Bazel Central Registry](https://registry.bazel.build/modules/gavel_tools).

You then approve that PR yourself — you are a listed module maintainer, so no BCR
maintainer review is needed once the first version is in and the `presubmit.yml`
is unchanged.

## One-time setup

Publishing opens a PR against the BCR fork, so it needs a `PUBLISH_TO_BCR_TOKEN`
secret in this repo — a classic PAT that can push to `gavelcode/bazel-central-registry`
and open pull requests. Set it under *Settings → Secrets and variables → Actions*.

## Why the tag drives everything

The version lives in exactly one place — `MODULE.bazel` — and the release script
is the only thing that edits it, so the tag and the module version can never
disagree. The registry entry is generated from the tag, never written by hand.

## The gavel registry is frozen

Earlier versions were also mirrored to a separate [gavel registry](https://github.com/gavelcode/registry)
(versions `0.1.0`–`0.3.10`). Now that gavel_tools is in the Bazel Central
Registry — the default registry every Bazel user already reads — that mirror is
frozen: no new versions are published to it. Consumers need no custom `--registry`
line; a plain `bazel_dep` resolves against BCR.
