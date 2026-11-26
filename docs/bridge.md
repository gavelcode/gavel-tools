---
title: Bridges — bring your own linter
type: reference
description: The stable API and SARIF contract for writing a Bazel aspect that gavel builds and judges for a linter gavel-tools does not ship.
resource: https://github.com/gavelcode/gavel-tools/blob/main/lint/bridge.bzl
tags: [bridge, aspect, sarif, contract, extension]
---

# Bridges — bring your own linter

gavel-tools guarantees hermetic execution and SARIF-with-`ruleId` for the tools in
its [catalog](catalog.md). For any other tool you write a **bridge**: an aspect you
own that returns SARIF in the `gavel_submissions` output group (the
[SARIF boundary](sarif-boundary.md)). gavel builds that aspect itself over the
project's targets — fresh, cached, scoped — and reports its findings as *provided
by the project, not guaranteed by gavel*. gavel never reads arbitrary directories
(that evidence could be stale); the output group is the whole contract.

## The API

Public and stable: `@gavel_tools//lint:bridge.bzl`. Its docstrings are the
reference for each symbol (`gavel_submission`, `gavel_forward`,
`gavel_output_name`, `GAVEL_ATTR_ASPECTS`, `GAVEL_SUBMISSIONS_OUTPUT_GROUP`).
Stability follows the module's semver; while the module is alpha (see the
[README](../README.md)) a breaking change lands only in a minor or major release,
never a patch. Everything else under `lint/aspects/` is internal.

Rules of the road: return `gavel_submission` for a target you analysed and
`gavel_forward` for every other target the aspect visits, and set
`attr_aspects = GAVEL_ATTR_ASPECTS`, so findings of dependencies are collected too.

## Minimal example

The tested, runnable version is [`examples/bridge/todo.bzl`](../examples/bridge/todo.bzl):

```starlark
load("@gavel_tools//lint:bridge.bzl", "GAVEL_ATTR_ASPECTS", "gavel_forward", "gavel_output_name", "gavel_submission")

def _impl(target, ctx):
    if ctx.rule.kind != "my_library":
        return gavel_forward(ctx)
    sarif = ctx.actions.declare_file(gavel_output_name(ctx.label, ".mytool.sarif"))
    ctx.actions.run(...)  # your hermetic action writes SARIF to `sarif`
    return gavel_submission(ctx, sarif)

my_aspect = aspect(implementation = _impl, attr_aspects = GAVEL_ATTR_ASPECTS)
```

Declare the tool in `gavel.yaml` with the aspect label and the `sarif_suffix` your
files end with (here `.mytool.sarif`); gavel selects results by that suffix.

## SARIF contract

<!-- BEGIN CONTRACT: validated by sarif.ValidateBridge in lint/sarif/validate.go; the code wins on any disagreement -->
Each submitted file is one SARIF 2.1.0 JSON document, and its file name ends with
the `sarif_suffix` declared in `gavel.yaml`.

- `runs` has at least one run. `runs[].tool.driver.name` is non-empty and
  identifies the tool.
- Every result has:
  - `ruleId`: non-empty;
  - `message.text`: non-empty;
  - `locations[0].physicalLocation.artifactLocation.uri`: non-empty and
    workspace-relative (no scheme such as `file://`, no leading `/`, no `..`
    segment, no `execroot/` path);
  - `locations[0].physicalLocation.region.startLine`: an integer >= 1;
  - `level`: one of `error`, `warning`, `note`, `none`; when absent it is
    treated as `warning`.
- A run that could not complete reports it in `runs[].invocations[]` with
  `executionSuccessful: false` and at least one `toolExecutionNotifications`
  entry whose `message.text` gives the reason. A run that completed has
  `executionSuccessful: true`; warning-level notifications mark partial coverage.
- gavel computes its own finding fingerprint (from tool, `ruleId`, file and line
  content); fingerprints in the SARIF are optional.
<!-- END CONTRACT -->

To check your output the same way gavel-tools does, run
`bazel run @gavel_tools//lint/cmd/gavel-lint -- validate-bridge FILE.sarif...`
(prints each violation as `file: message`; exit 0 valid, 1 violations, 2 usage or
I/O error), or call `ValidateBridge` from
`github.com/gavelcode/gavel-tools/lint/sarif` (see
[`examples/bridge/bridge_test.go`](../examples/bridge/bridge_test.go)).
