"""Rust submission aspects: Clippy (via rules_rust) and architecture checks.

Both run sandboxed. Clippy reuses rules_rust's upstream hermetic aspect and only
converts its diagnostics to SARIF; archtest is a pure source-parsing wrapper
whose only inputs (the .rs sources and .gavel/architecture.yml) are declared on
the action.
"""

load("@rules_rust//rust:defs.bzl", _rust_clippy_aspect_upstream = "rust_clippy_aspect")
load(
    ":common.bzl",
    _collect_dep_submissions = "collect_dep_submissions",
    _empty_output_groups = "empty_output_groups",
    _lint_config_files = "lint_config_files",
    _safe_output_name = "safe_output_name",
    _submission_output_groups = "submission_output_groups",
)

def _collect_rust_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension == "rs"]

def _rust_clippy_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]
    if OutputGroupInfo not in target:
        return [_empty_output_groups(transitive)]
    og = target[OutputGroupInfo]

    # rules_rust surfaces Clippy diagnostics under one of two output-group names
    # depending on its version and the consumer's clippy settings. Prefer the
    # structured `clippy_output` the converter expects, and fall back to
    # `clippy_checks`, so the aspect works whatever rules_rust the consumer pins.
    if not hasattr(og, "clippy_output"):
        if not hasattr(og, "clippy_checks"):
            return [_empty_output_groups(transitive)]
        clippy_files = og.clippy_checks.to_list()
        if not clippy_files:
            return [_empty_output_groups(transitive)]
        diagnostics_file = clippy_files[0]
    else:
        diag_files = og.clippy_output.to_list()
        if not diag_files:
            return [_empty_output_groups(transitive)]
        diagnostics_file = diag_files[0]
    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".clippy.sarif")
    ctx.actions.run(
        executable = ctx.executable._clippy_converter,
        inputs = [diagnostics_file],
        outputs = [output],
        arguments = [
            "--in",
            diagnostics_file.path,
            "--out",
            output.path,
        ],
        mnemonic = "GavelRustClippySARIF",
        progress_message = "Converting Clippy output to SARIF for %s" % ctx.label,
    )

    return [_submission_output_groups(output, transitive)]

rust_clippy_submission_aspect = aspect(
    implementation = _rust_clippy_aspect_impl,
    requires = [_rust_clippy_aspect_upstream],
    attr_aspects = [
        "deps",
        "proc_macro_deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_clippy_converter": attr.label(
            default = Label("//lint/lang/rust/clippy/converter/cmd:converter"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _rust_archtest_aspect_impl(_target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_rust_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".archtest.sarif")
    ctx.actions.run(
        executable = ctx.executable._archtest_wrapper,
        inputs = srcs + _lint_config_files(ctx),
        outputs = [output],
        arguments = [
            "--config",
            ".gavel/architecture.yml",
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelRustArchTest",
        progress_message = "Checking Rust architecture for %s" % ctx.label,
    )

    return [_submission_output_groups(output, transitive)]

rust_archtest_submission_aspect = aspect(
    implementation = _rust_archtest_aspect_impl,
    attr_aspects = [
        "deps",
        "proc_macro_deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_archtest_wrapper": attr.label(
            default = Label("//lint/lang/rust/archtest/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)
