---
title: Releasing
type: reference
description: One command tags a version and publishes it to the gavel registry.
---

# Releasing

A release is one command, from `main`:

```bash
bash .github/ci/release.sh X.Y.Z
```

It bumps the `MODULE.bazel` version, pushes it, waits for CI to pass on that
exact commit, then tags `vX.Y.Z`. The tag drives
[`release.yml`](../.github/workflows/release.yml), which computes the tag
tarball's integrity and adds `modules/gavel_tools/X.Y.Z/` to the
[registry](https://github.com/gavelcode/registry). There is nothing else to do.

## One-time setup

Publishing writes to the separate registry repo, so it needs a `REGISTRY_TOKEN`
secret in this repo — a token with **Contents: write** on `gavelcode/registry`,
set under *Settings → Secrets and variables → Actions*.

## Why the tag drives everything

The version lives in exactly one place — `MODULE.bazel` — and the release script
is the only thing that edits it, so the tag and the module version can never
disagree. The registry entry is generated from the tag, never written by hand;
the hand-authored 0.1.0 entry was the exception this automation replaces.
