load(
    ":common.bzl",
    _collect_dep_submissions = "collect_dep_submissions",
    _empty_output_groups = "empty_output_groups",
    _lint_config_files = "lint_config_files",
    _safe_output_name = "safe_output_name",
    _submission_output_groups = "submission_output_groups",
)

# Per-language aspects live in <lang>.bzl. They are loaded under private aliases
# and re-bound to public names below: a bare `load()` symbol is NOT exported for
# the `defs.bzl%<aspect>` reference, but a top-level assignment is. This keeps
# the public entry points (in every consumer's .bazelrc) stable as languages
# move to per-language files.
load(
    ":rust.bzl",
    _rust_archtest_submission_aspect = "rust_archtest_submission_aspect",
    _rust_clippy_submission_aspect = "rust_clippy_submission_aspect",
)
load(
    ":python.bzl",
    _python_archtest_submission_aspect = "python_archtest_submission_aspect",
    _python_bandit_submission_aspect = "python_bandit_submission_aspect",
    _python_pycompile_submission_aspect = "python_pycompile_submission_aspect",
    _python_ruff_submission_aspect = "python_ruff_submission_aspect",
)
load(
    ":java.bzl",
    _java_archtest_submission_aspect = "java_archtest_submission_aspect",
    _java_cpd_submission_aspect = "java_cpd_submission_aspect",
    _java_error_prone_submission_aspect = "java_error_prone_submission_aspect",
    _java_pmd_submission_aspect = "java_pmd_submission_aspect",
    _java_spotbugs_submission_aspect = "java_spotbugs_submission_aspect",
)
load(
    ":go.bzl",
    _go_archtest_submission_aspect = "go_archtest_submission_aspect",
    _go_golangci_lint_submission_aspect = "go_golangci_lint_submission_aspect",
)

go_golangci_lint_submission_aspect = _go_golangci_lint_submission_aspect

go_archtest_submission_aspect = _go_archtest_submission_aspect

rust_clippy_submission_aspect = _rust_clippy_submission_aspect

rust_archtest_submission_aspect = _rust_archtest_submission_aspect

python_pycompile_submission_aspect = _python_pycompile_submission_aspect

python_ruff_submission_aspect = _python_ruff_submission_aspect

python_bandit_submission_aspect = _python_bandit_submission_aspect

python_archtest_submission_aspect = _python_archtest_submission_aspect

java_pmd_submission_aspect = _java_pmd_submission_aspect

java_cpd_submission_aspect = _java_cpd_submission_aspect

java_spotbugs_submission_aspect = _java_spotbugs_submission_aspect

java_error_prone_submission_aspect = _java_error_prone_submission_aspect

java_archtest_submission_aspect = _java_archtest_submission_aspect

def _collect_typescript_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension in ("ts", "tsx")]

def _typescript_eslint_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_typescript_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".eslint.sarif")
    ctx.actions.run(
        executable = ctx.executable._eslint_sarif,
        inputs = srcs + _lint_config_files(ctx),
        outputs = [output],
        arguments = [
            "--eslint",
            ctx.executable._eslint.path,
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        env = {"BAZEL_BINDIR": ctx.bin_dir.path},
        mnemonic = "GavelTypeScriptESLintSARIF",
        progress_message = "Generating ESLint submission for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        tools = [ctx.executable._eslint],
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

typescript_eslint_submission_aspect = aspect(
    implementation = _typescript_eslint_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_eslint": attr.label(
            default = Label("//lint/lang/typescript/eslint"),
            executable = True,
            cfg = "exec",
        ),
        "_eslint_sarif": attr.label(
            default = Label("//lint/lang/typescript/eslint/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)

# --- Architecture validation aspects ---

def _typescript_archtest_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_typescript_srcs(ctx)
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
        mnemonic = "GavelTypeScriptArchTest",
        progress_message = "Checking TypeScript architecture for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

typescript_archtest_submission_aspect = aspect(
    implementation = _typescript_archtest_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_archtest_wrapper": attr.label(
            default = Label("//lint/lang/typescript/archtest/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)
