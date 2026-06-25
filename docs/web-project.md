---
title: web_project macro
type: reference
summary: One macro that generates a frontend app's whole Bazel build graph.
---

# web_project macro

`@gavel_tools//macros:web.bzl%web_project` generates a frontend app's entire
Bazel build graph — esbuild bundle + tailwind css + production `dist/` copies +
`tsc` type-check + eslint — from a single declaration, so consumers stop
hand-wiring ~180 lines of `copy_file` + bundler + tool glue.

The per-tool npm binaries (eslint / tsc / tailwind) come from the **consumer's**
own lockfile and are passed in, so tool versions stay consumer-owned.

## Generated targets (for `name = "web"`)

| Target | Purpose | Verb |
|--------|---------|------|
| `:web` | production build filegroup (`dist/*`) | `bazel build` |
| `:web.bundle` | esbuild bundle (intermediate) | |
| `:web.styles` | tailwind css (intermediate, optional) | |
| `:web.typecheck` | `tsc --noEmit` | `bazel test` |
| `:web.lint` | eslint | `bazel test` / `gavel judge` |

There is **no** runnable target: a frontend is served embedded by the backend,
and dev (watch/HMR) is run outside Bazel.

## Usage

```python
load("@gavel_tools//macros:web.bzl", "web_project")
load("@npm//:defs.bzl", "npm_link_all_packages")
load("@npm//apps/web:eslint/package_json.bzl", eslint_bin = "bin")
load("@npm//apps/web:typescript/package_json.bzl", tsc_bin = "bin")
load("@npm//apps/web:tailwindcss/package_json.bzl", tailwind_bin = "bin")

npm_link_all_packages(name = "node_modules")

web_project(
    name = "web",
    entry_point = "src/main.tsx",
    tsconfig = "tsconfig.app.json",
    eslint = eslint_bin,
    tsc = tsc_bin,
    tailwind = tailwind_bin,
    runtime_deps = ["react", "react-dom", ...],
    type_deps = ["typescript", "@types/react", ...],
    lint_deps = ["eslint", "typescript-eslint", ...],
    css_deps = ["tailwindcss", "autoprefixer", ...],
    server_visibility = ["//apps/server:__subpackages__"],
)
```

Owning build complexity for the painful languages — not just linting — is part of
this module's mission.

## Open: hermetic type-aware ESLint

Type-aware ESLint (the powerful `@typescript-eslint` rules) needs the consumer to
expose its tsconfig + type/plugin npm closure as Bazel inputs — a
`js_lib_helpers.gather_files_from_js_infos` gather in the eslint aspect, plus the
closure declared by the consumer (which `web_project` already declares via
`type_deps` / `lint_deps` / `tsconfig`). The aspect-side gather is WIP (stashed);
the consumer-convention layer is the remaining work. Until then, `:web.lint` runs
the existing non-type-aware eslint aspect.
