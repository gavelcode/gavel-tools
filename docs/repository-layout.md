---
title: Repository layout
type: reference
description: The lint/ + macros/ structure, the resulting labels, and tool-binary ownership.
---

# Repository layout

The module does two things — **lint** and **scaffold builds** — and the root
reflects exactly those two categories. (`tools/` was a monorepo artifact and was
removed; nothing is nested under a redundant segment.)

```
gavel-tools/
├── MODULE.bazel  BUILD.bazel  go.mod  go.sum  README.md  LICENSE
├── docs/                              # this doc bundle
│
├── lint/                             # LINTERS → consumed via --aspects
│   ├── catalog.yaml                  #   language→tools menu (planned — see catalog.md)
│   ├── aspects/                      #   lint aspects: one <lang>.bzl each; defs.bzl re-exports them
│   ├── archtest/                     #   shared Go arch-rules library
│   └── lang/                         #   per-language wrappers + tool repos
│       ├── go/golangci_lint/
│       ├── java/{pmd,spotbugs,error_prone,cpd}/
│       ├── python/{ruff,bandit,pycompile}/
│       ├── rust/clippy/
│       └── typescript/eslint/
│
└── macros/                           # BUILD MACROS → consumed via load()
    └── web.bzl                       #   web_project (frontend build graph)
```

## Labels

- `@gavel_tools//lint/aspects:defs.bzl%<lang>_<tool>_submission_aspect`
- `@gavel_tools//lint/lang/go/golangci_lint:repositories.bzl`
- `@gavel_tools//lint:catalog.yaml` (planned)
- `@gavel_tools//macros:web.bzl%web_project`

The root holds only `lint/`, `macros/`, `docs/` and the module files — separated
by *kind* (linters vs build macros), and within `lint/` by *role* (menu / engine
/ shared lib / languages). "Is `rust` a language or a macro?" is unambiguous:
`rust` is a language under `lint/lang/`; `web` is a macro under `macros/`.

## Tool-binary ownership

gavel-tools declares the linter tool binary repos (`@golangci_lint`, `@pmd`,
`@spotbugs`, `@error_prone`, `@ruff`, `@bandit`) in its `MODULE.bazel` via
`use_repo_rule`. This is required: repos a consumer declares are **not visible
across the module boundary** to the aspect that references them. Versions live
here, centrally — consumers do not manage them.
