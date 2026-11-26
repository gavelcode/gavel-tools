"""Ruleset-agnostic factory for the Rust Clippy submission aspect.

Deliberately does NOT load @rules_rust: gavel_tools must not impose a Rust
ruleset on consumers, so the upstream Clippy aspect is injected by whoever binds
it to the ruleset that actually builds the consumer's targets (core binds
rules_rust; @gavel_tools_rust_rs binds the rules_rs facade).
"""

load(
    ":common.bzl",
    "GAVEL_LINT_TOOLCHAIN_TYPE",
    "gavel_lint_binary",
    _collect_dep_submissions = "collect_dep_submissions",
    _empty_output_groups = "empty_output_groups",
    _safe_output_name = "safe_output_name",
    _submission_output_groups = "submission_output_groups",
)

# Rule kinds that compile Rust sources and so must yield Clippy output.
_RUST_KINDS = [
    "rust_library",
    "rust_binary",
    "rust_test",
    "rust_proc_macro",
    "rust_shared_library",
    "rust_static_library",
]

_MISSING_OUTPUT = (
    "clippy output was not produced for {label}: the Rust Clippy aspect may be " +
    "bound to a different Rust ruleset than the one building the target. " +
    "Use the default rust_clippy_submission_aspect (@gavel_tools, built on " +
    "rules_rust) for rules_rust targets, or the rules_rs variant " +
    "(@gavel_tools_rust_rs, built on rules_rs) for rules_rs targets."
)

def _has_rust_srcs(ctx):
    if not hasattr(ctx.rule.attr, "srcs"):
        return False
    for src in ctx.rule.attr.srcs:
        for f in src.files.to_list():
            if f.extension == "rs":
                return True
    return False

def _fail_loudly(ctx, transitive):
    """Emits a failed-execution SARIF (shape of sarif.WriteFailed) instead of silence."""
    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".clippy.sarif")
    ctx.actions.write(output, json.encode_indent({
        "version": "2.1.0",
        "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
        "runs": [{
            "tool": {"driver": {"name": "clippy"}},
            "results": [],
            "invocations": [{
                "executionSuccessful": False,
                "toolExecutionNotifications": [{
                    "level": "error",
                    "message": {"text": _MISSING_OUTPUT.format(label = str(ctx.label))},
                }],
            }],
        }],
    }) + "\n")
    return [_submission_output_groups(output, transitive)]

def _skipped(target, ctx, transitive):
    """Skips quietly unless a Rust target in the main repo should have had output."""
    if ctx.label.workspace_name or ctx.rule.kind not in _RUST_KINDS or not _has_rust_srcs(ctx):
        return [_empty_output_groups(transitive)]
    return _fail_loudly(ctx, transitive)

def _impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]
    if OutputGroupInfo not in target:
        return _skipped(target, ctx, transitive)
    og = target[OutputGroupInfo]

    # rules_rust surfaces Clippy diagnostics under one of two output-group names
    # depending on its version and the consumer's clippy settings. Prefer the
    # structured `clippy_output` the converter expects, and fall back to
    # `clippy_checks`, so the aspect works whatever rules_rust the consumer pins.
    if not hasattr(og, "clippy_output"):
        if not hasattr(og, "clippy_checks"):
            return _skipped(target, ctx, transitive)
        clippy_files = og.clippy_checks.to_list()
        if not clippy_files:
            return _skipped(target, ctx, transitive)
        diagnostics_file = clippy_files[0]
    else:
        diag_files = og.clippy_output.to_list()
        if not diag_files:
            return _skipped(target, ctx, transitive)
        diagnostics_file = diag_files[0]
    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".clippy.sarif")
    ctx.actions.run(
        executable = gavel_lint_binary(ctx),
        inputs = [diagnostics_file],
        outputs = [output],
        arguments = [
            "clippy-convert",
            "--in",
            diagnostics_file.path,
            "--out",
            output.path,
        ],
        mnemonic = "GavelRustClippySARIF",
        progress_message = "Converting Clippy output to SARIF for %s" % ctx.label,
    )

    return [_submission_output_groups(output, transitive)]

def make_rust_clippy_submission_aspect(upstream_clippy_aspect):
    """Builds the Clippy submission aspect on top of a ruleset's upstream aspect.

    Args:
        upstream_clippy_aspect: the `rust_clippy_aspect` of the Rust ruleset that
            builds the consumer's targets (rules_rust or the rules_rs facade).

    Returns:
        An aspect converting that ruleset's Clippy diagnostics to SARIF, failing
        loudly when a Rust target yields none.
    """
    return aspect(
        implementation = _impl,
        toolchains = [GAVEL_LINT_TOOLCHAIN_TYPE],
        requires = [upstream_clippy_aspect],
        attr_aspects = [
            "deps",
            "proc_macro_deps",
        ],
        attrs = {
            "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        },
    )
