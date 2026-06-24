# gavel-tools

Bazel linting machinery for [Gavel](https://github.com/gavelcode/gavel): Starlark
aspects that run static analyzers as Bazel aspects and normalize their output to
SARIF, per-language Go wrappers, and a shared architecture-test library.

Consumed as a Bazel module (`bazel_dep(name = "gavel_tools")`). The interface to
Gavel is SARIF files on disk — there are no Go imports across the boundary.

## Languages

| Language   | Aspects |
|------------|---------|
| Go         | golangci-lint, archtest |
| Java       | PMD, CPD, SpotBugs, Error Prone, archtest |
| Python     | Ruff, Bandit, pycompile, archtest |
| TypeScript | ESLint, archtest |
| Rust       | Clippy, archtest |

## License

Apache 2.0. See [LICENSE](LICENSE).
