"""TypeScript submission aspects: ESLint and archtest.

ESLint runs **sandboxed and hermetic**: the aspect runs on rules_js library
targets (`js_library`, `ts_project`) and harvests the consumer's ESLint plugins
from `JsInfo.npm_sources` — the rules_js equivalent of how the Go aspect harvests
the package graph — declaring them, the sources and the flat config as action
inputs. gavel's pinned ESLint then resolves the consumer's plugins inside the
sandbox (see docs/tier-model.md). archtest is a pure source-parsing wrapper.
"""

load("@aspect_rules_js//js:providers.bzl", "JsInfo")
load(
    ":common.bzl",
    _collect_dep_submissions = "collect_dep_submissions",
    _empty_output_groups = "empty_output_groups",
    _lint_config_files = "lint_config_files",
    _safe_output_name = "safe_output_name",
    _submission_output_groups = "submission_output_groups",
)

_ESLINT_CONFIG_PREFIX = "eslint.config."

def _collect_typescript_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension in ("ts", "tsx")]

def _find_eslint_config(sources):
    for src in sources:
        if src.basename.startswith(_ESLINT_CONFIG_PREFIX):
            return src
    return None

def _typescript_eslint_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]
    if JsInfo not in target:
        return [_empty_output_groups(transitive)]

    js = target[JsInfo]
    sources = js.sources.to_list()
    ts_srcs = [s for s in sources if s.extension in ("ts", "tsx")]
    if not ts_srcs:
        return [_empty_output_groups(transitive)]

    config = _find_eslint_config(sources)

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".eslint.sarif")
    args = [
        "--eslint",
        ctx.executable._eslint.path,
        "--out",
        output.path,
    ]
    if config:
        args += ["--config", config.path]
    args += [src.path for src in ts_srcs]

    config_inputs = [config] if config else []
    ctx.actions.run(
        executable = ctx.executable._eslint_sarif,
        inputs = depset(
            direct = ts_srcs + config_inputs + _lint_config_files(ctx),
            transitive = [js.npm_sources, js.transitive_sources],
        ),
        outputs = [output],
        arguments = args,
        env = {"BAZEL_BINDIR": ctx.bin_dir.path},
        mnemonic = "GavelTypeScriptESLintSARIF",
        progress_message = "Generating ESLint submission for %s" % ctx.label,
        tools = [ctx.executable._eslint],
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
