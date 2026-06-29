"""Python submission aspects: pycompile, Ruff, Bandit and architecture checks.

All run sandboxed — every input is declared on the action. Ruff is a
self-contained binary; Bandit and pycompile run under the consumer's hermetic
rules_python interpreter (declared as an input) instead of a PATH python3;
archtest is a pure source-parsing wrapper.
"""

load("@rules_python//python:defs.bzl", "PyInfo")
load(
    ":common.bzl",
    _collect_dep_submissions = "collect_dep_submissions",
    _empty_output_groups = "empty_output_groups",
    _lint_config_files = "lint_config_files",
    _safe_output_name = "safe_output_name",
    _submission_output_groups = "submission_output_groups",
)

def _collect_python_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension == "py"]

def _python_pycompile_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if PyInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_python_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    # py_compile needs a real interpreter; take the hermetic one from the
    # consumer's rules_python toolchain instead of a PATH python3, and declare it
    # as an input so the action stays sandboxed.
    runtime = ctx.toolchains["@rules_python//python:toolchain_type"].py3_runtime
    interpreter_inputs = []
    interpreter_transitive = []
    if runtime.interpreter:
        python_path = runtime.interpreter.path
        interpreter_inputs = [runtime.interpreter]
        interpreter_transitive = [runtime.files]
    else:
        python_path = runtime.interpreter_path

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".pycompile.sarif")
    ctx.actions.run(
        executable = ctx.executable._python_pycompile_sarif,
        inputs = depset(
            direct = srcs + _lint_config_files(ctx) + interpreter_inputs,
            transitive = interpreter_transitive,
        ),
        outputs = [output],
        arguments = [
            "--python",
            python_path,
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelPythonPyCompileSARIF",
        progress_message = "Generating Python py_compile submission for %s" % ctx.label,
    )

    return [_submission_output_groups(output, transitive)]

python_pycompile_submission_aspect = aspect(
    implementation = _python_pycompile_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    toolchains = ["@rules_python//python:toolchain_type"],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_python_pycompile_sarif": attr.label(
            default = Label("//lint/lang/python/pycompile/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _python_ruff_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if PyInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_python_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".ruff.sarif")
    ctx.actions.run(
        executable = ctx.executable._ruff_sarif,
        inputs = srcs + _lint_config_files(ctx),
        outputs = [output],
        arguments = [
            "--ruff",
            ctx.file._ruff.path,
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelPythonRuffSARIF",
        progress_message = "Generating Ruff submission for %s" % ctx.label,
        tools = [ctx.file._ruff],
    )

    return [_submission_output_groups(output, transitive)]

python_ruff_submission_aspect = aspect(
    implementation = _python_ruff_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_ruff": attr.label(
            default = Label("@ruff//:ruff"),
            allow_single_file = True,
            cfg = "exec",
        ),
        "_ruff_sarif": attr.label(
            default = Label("//lint/lang/python/ruff/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _python_bandit_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if PyInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_python_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    site_packages = ctx.attr._bandit_packages.files.to_list()
    site_packages_dir = site_packages[0].path.split("/site-packages/")[0] + "/site-packages" if site_packages else ""

    # Bandit (via stevedore) needs Python 3.10+, so run it under the hermetic
    # interpreter from the consumer's rules_python toolchain rather than
    # whatever python3 happens to be on PATH.
    runtime = ctx.toolchains["@rules_python//python:toolchain_type"].py3_runtime
    interpreter_inputs = []
    interpreter_transitive = []
    if runtime.interpreter:
        python_path = runtime.interpreter.path
        interpreter_inputs = [runtime.interpreter]
        interpreter_transitive = [runtime.files]
    else:
        python_path = runtime.interpreter_path

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".bandit.sarif")
    ctx.actions.run(
        executable = ctx.executable._bandit_sarif,
        inputs = depset(
            direct = srcs + site_packages + _lint_config_files(ctx) + interpreter_inputs,
            transitive = interpreter_transitive,
        ),
        outputs = [output],
        arguments = [
            "--python",
            python_path,
            "--site-packages",
            site_packages_dir,
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelPythonBanditSARIF",
        progress_message = "Generating Bandit submission for %s" % ctx.label,
    )

    return [_submission_output_groups(output, transitive)]

python_bandit_submission_aspect = aspect(
    implementation = _python_bandit_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    toolchains = ["@rules_python//python:toolchain_type"],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_bandit_packages": attr.label(
            default = Label("@bandit//:site_packages"),
        ),
        "_bandit_sarif": attr.label(
            default = Label("//lint/lang/python/bandit/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _python_archtest_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if PyInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_python_srcs(ctx)
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
        mnemonic = "GavelPythonArchTest",
        progress_message = "Checking Python architecture for %s" % ctx.label,
    )

    return [_submission_output_groups(output, transitive)]

python_archtest_submission_aspect = aspect(
    implementation = _python_archtest_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_archtest_wrapper": attr.label(
            default = Label("//lint/lang/python/archtest/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)
