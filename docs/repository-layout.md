---
title: Repository layout
type: reference
description: One Bazel module per language under modules/, the core module at the root, the resulting labels, and tool-binary ownership.
resource: https://github.com/gavelcode/gavel-tools
tags: [layout, bazel, structure, modules]
---

# Repository layout

gavel-tools **owns** the linters, their versions, hermetic execution and the SARIF
shape (see [tier-model](tier-model.md) and [sarif-boundary](sarif-boundary.md)). It
must **not** impose anything else on a consumer: neither their language rulesets and
toolchains, nor anything for a language they do not lint. So the repository is
several Bazel modules released together, and a consumer's module graph and
`MODULE.bazel.lock` hold only the rulesets and linters of the languages it uses.

```
gavel-tools/
├── MODULE.bazel  BUILD.bazel  go.mod  go.sum  README.md  LICENSE   # core module `gavel_tools`
├── lint/                             # CORE: no language ruleset
│   ├── catalog.yaml                  #   language→tools menu, each tool naming its `module` (see catalog.md)
│   ├── aspects/                      #   common.bzl helpers, the ruleset-agnostic Clippy factory, Rust archtest
│   ├── archtest/ sarif/              #   Go libraries compiled into gavel-lint
│   ├── cmd/gavel-lint/               #   the single multi-call helper binary (`gavel-lint <tool>`)
│   ├── toolchain/                    #   gavel-lint toolchain: prebuilt per platform (release) / source (dev)
│   └── lang/<lang>/<tool>/           #   Go wrapper packages (compiled into gavel-lint)
├── modules/                          # one module per language: aspects + linter binaries, nothing else
│   ├── go/          gavel_tools_go          rules_go          golangci-lint
│   ├── java/        gavel_tools_java        rules_java        PMD, CPD, SpotBugs, Error Prone
│   ├── python/      gavel_tools_python      rules_python      Ruff, Bandit
│   ├── typescript/  gavel_tools_typescript  aspect_rules_js   ESLint (gavel-owned npm lock)
│   ├── rust/        gavel_tools_rust        rules_rust        Clippy via rules_rust
│   ├── rust_rs/     gavel_tools_rust_rs     rules_rs          Clippy via rules_rs
│   └── web/         gavel_tools_web         rules_js, esbuild, bazel_lib   `web_project` macro
└── docs/                             # this doc bundle
```

Each language module has the same shape (`MODULE.bazel`, `lint/aspects/{defs.bzl,<lang>.bzl}`,
`lint/lang/<lang>/<tool>/repositories.bzl` for its binaries) and depends on the core
plus only its own ruleset, at the minimum version it needs (the consumer's own
version wins, and no toolchain is ever registered). The exact dependencies are in
each `modules/<lang>/MODULE.bazel`; `//lint:lint_test` enforces that the core and
each module carry no ruleset but their own, that the aspects each `defs.bzl` exports
match the [catalog](../lint/catalog.yaml), and that every module shares one version.

## Labels

- `@<module>//lint/aspects:defs.bzl%<lang>_<tool>_submission_aspect`, `<module>` as the
  catalog names it (`gavel_tools` for the Rust archtest, which parses sources and loads
  no ruleset, so it serves rules_rust and rules_rs consumers alike)
- `@gavel_tools//lint:catalog.yaml`
- `@gavel_tools_web//macros:web.bzl%web_project`

## Why these boundaries

- **One module per language**, going further than rules_lint (which keeps most languages
  in one module) the way bazel-lib splits tar/jq/yq: a Rust-only consumer never resolves
  rules_go, rules_java, rules_python or aspect_rules_js, and its lockfile never names them.
- **Directory scheme `modules/<lang>/`**: the root stays the core module, the layout is
  uniform and `.bazelignore` hides `modules/` from `//...` of the core.
- **`web_project` is its own module**, not part of `gavel_tools_typescript`: it needs
  esbuild and bazel-lib, which a consumer that only lints TypeScript must not pull. It does
  not depend on the core either — the per-tool binaries come from the consumer's lockfile.
- **rules_go in the core** is only for building gavel-lint from source and the Go tests, so it
  is a `dev_dependency = IS_RELEASE` (as gazelle and the Go SDK are): releases ship prebuilt
  binaries and carry no rules_go; a source checkout used directly still resolves it.
- The root declares the language modules as dev-only dependencies with
  `local_path_override`, only so in-repo tests can read them (the graph is cyclic by
  design); consumers ignore them.

## Tool-binary ownership

Each language module declares its linter binary repos (`@golangci_lint`, `@pmd`, `@spotbugs`,
`@error_prone`, `@ruff`, `@bandit`, `@gavel_eslint`) in its own `MODULE.bazel` — repos a
consumer declares are **not visible across the module boundary** to the aspect that references
them. Versions live there, centrally; consumers do not manage them.
