# Security Policy

## Supported versions

gavel-tools is **alpha**. Security fixes land on `main`; there are no
maintained release branches yet. Pin a commit via `git_override` and update to
pick up fixes.

## Reporting a vulnerability

Please **do not open a public issue** for security problems.

Use GitHub's **private vulnerability reporting**: go to the repository's
**Security** tab → **Report a vulnerability**. That opens a private advisory
thread visible only to the maintainers.

gavel-tools runs static analyzers as part of a Bazel build; the most relevant
surface is build-time execution (aspect wrappers, tool binaries resolved by
Bazel) rather than a running service. Reports about tool-binary integrity,
sandbox escapes, or supply-chain concerns in the declared tool repositories are
in scope.

You can expect an initial response within a few days.
