"""Rust architecture submission aspect.

Pure source parsing: its only inputs (the .rs sources and .gavel/architecture.yml)
are declared on the action, and it loads no Rust ruleset, which is why it lives
in the core module and serves consumers on rules_rust and rules_rs alike. The
Clippy aspect, which does need a ruleset, is published by gavel_tools_rust and
gavel_tools_rust_rs (both built from rust_clippy.bzl).
"""

load(
    ":common.bzl",
    "GAVEL_LINT_TOOLCHAIN_TYPE",
    "gavel_lint_binary",
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

GavelRustCrateInfo = provider(
    doc = "Crate name and Bazel package of a Rust target, as seen by the archtest aspect.",
    fields = {
        "crate_name": "Rust crate name as written in `use` paths; empty for external-repo targets.",
        "package": "Workspace-relative Bazel package of the target.",
    },
)

def _crate_name(ctx):
    """The name `use` paths refer to: the explicit crate_name, else the target name.

    Mirrors rules_rust's defaulting without reading its CrateInfo, so the aspect
    works with rules_rs and any other rules_rust redistribution.
    """
    explicit = getattr(ctx.rule.attr, "crate_name", "")
    if explicit:
        return explicit
    return ctx.label.name.replace("-", "_")

def _dep_crate_flags(ctx):
    """Builds repeated `--crate name=package` flags for the direct dependencies."""
    flags = []
    seen = {}
    for attr_name in ["deps", "proc_macro_deps"]:
        for dep in getattr(ctx.rule.attr, attr_name, []):
            # A dep the aspect did not visit (e.g. through an alias) has no provider.
            if GavelRustCrateInfo not in dep:
                continue
            info = dep[GavelRustCrateInfo]
            if not info.crate_name or info.crate_name in seen:
                continue
            seen[info.crate_name] = True
            flags.extend(["--crate", "%s=%s" % (info.crate_name, info.package)])
    return flags

def _rust_archtest_aspect_impl(_target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if ctx.label.workspace_name:
        # Third-party crates stay unresolved (ignored) in dependents.
        return [_empty_output_groups(transitive), GavelRustCrateInfo(crate_name = "", package = "")]

    crate_info = GavelRustCrateInfo(crate_name = _crate_name(ctx), package = ctx.label.package)

    srcs = _collect_rust_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive), crate_info]

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".archtest.sarif")
    ctx.actions.run(
        executable = gavel_lint_binary(ctx),
        inputs = srcs + _lint_config_files(ctx),
        outputs = [output],
        arguments = [
            "rust-archtest",
            "--config",
            ".gavel/architecture.yml",
            "--out",
            output.path,
            "--package",
            ctx.label.package,
        ] + _dep_crate_flags(ctx) + [src.path for src in srcs],
        mnemonic = "GavelRustArchTest",
        progress_message = "Checking Rust architecture for %s" % ctx.label,
    )

    return [_submission_output_groups(output, transitive), crate_info]

rust_archtest_submission_aspect = aspect(
    implementation = _rust_archtest_aspect_impl,
    toolchains = [GAVEL_LINT_TOOLCHAIN_TYPE],
    provides = [GavelRustCrateInfo],
    attr_aspects = [
        "deps",
        "proc_macro_deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
    },
)
