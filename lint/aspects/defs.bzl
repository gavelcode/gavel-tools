load("@rules_go//go:def.bzl", "GoLibrary")
load("@rules_java//java/common:java_info.bzl", "JavaInfo")
load("@rules_python//python:defs.bzl", "PyInfo")
load("@rules_rust//rust:defs.bzl", _rust_clippy_aspect_upstream = "rust_clippy_aspect")

# Exposes a Go target's own .go source files so a sibling go_test in the same
# package can declare them as lint-action inputs. The compiled .x archive
# (export data) is interface-only and does not change on body-local edits, so
# tracking it alone leaves the go_test lint cache stale on implementation-only
# changes. Tracking the sibling library's sources fixes that.
GavelGoSrcInfo = provider(
    doc = "Go source files of a target, for sibling go_test lint cache invalidation.",
    fields = {
        "srcs": "list of this target's own .go source File objects",
        "package": "the target's package path, used to match siblings",
    },
)

def _empty_output_groups(transitive):
    return OutputGroupInfo(
        gavel_submissions = depset(transitive = transitive),
    )

def _submission_output_groups(output, transitive):
    submissions = depset(
        direct = [output],
        transitive = transitive,
    )
    return OutputGroupInfo(
        gavel_submissions = submissions,
    )

def _collect_dep_submissions(ctx):
    groups = []
    if hasattr(ctx.rule.attr, "deps"):
        for dep in ctx.rule.attr.deps:
            if OutputGroupInfo in dep and hasattr(dep[OutputGroupInfo], "gavel_submissions"):
                groups.append(dep[OutputGroupInfo].gavel_submissions)
    if hasattr(ctx.rule.attr, "embed"):
        for dep in ctx.rule.attr.embed:
            if OutputGroupInfo in dep and hasattr(dep[OutputGroupInfo], "gavel_submissions"):
                groups.append(dep[OutputGroupInfo].gavel_submissions)
    return groups

def _lint_config_files(ctx):
    if hasattr(ctx.attr, "_lint_config"):
        return ctx.attr._lint_config.files.to_list()
    return []

_LINT_CONFIG_ATTR = {
    "_lint_config": attr.label(
        default = Label("@@//:gavel_lint_config"),
    ),
}

def _collect_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension == "go"]

def _collect_java_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension == "java"]

def _collect_python_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension == "py"]

def _collect_typescript_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension in ("ts", "tsx")]

def _java_runtime_output_jars(target):
    return [jar for jar in target[JavaInfo].runtime_output_jars if jar.extension == "jar"]

def _safe_output_name(label):
    return (label.package + "_" + label.name).replace("/", "_")

def _is_go_lint_target(target, ctx):
    if GoLibrary in target:
        return True
    if ctx.rule.kind == "go_test":
        return True
    return False

def _collect_dep_outputs(ctx):
    dep_files = []
    for attr_name in ["deps", "embed"]:
        if hasattr(ctx.rule.attr, attr_name):
            for dep in getattr(ctx.rule.attr, attr_name):
                if GoLibrary in dep:
                    dep_files.extend(dep.files.to_list())
    return dep_files

def _collect_sibling_srcs(ctx):
    sibling_srcs = []
    for attr_name in ["deps", "embed"]:
        if hasattr(ctx.rule.attr, attr_name):
            for dep in getattr(ctx.rule.attr, attr_name):
                if GavelGoSrcInfo in dep and dep[GavelGoSrcInfo].package == ctx.label.package:
                    sibling_srcs.extend(dep[GavelGoSrcInfo].srcs)
    return sibling_srcs

def _go_golangci_lint_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if not _is_go_lint_target(target, ctx):
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]
    if "/node_modules/" in ctx.label.package:
        return [_empty_output_groups(transitive)]

    srcs = _collect_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    is_test = ctx.rule.kind == "go_test"
    dep_files = _collect_dep_outputs(ctx) if is_test else []
    sibling_srcs = _collect_sibling_srcs(ctx) if is_test else []

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".golangci.sarif")
    args = [
        "--golangci-lint",
        ctx.file._golangci_lint.path,
        "--package",
        ctx.label.package,
        "--out",
        output.path,
    ]
    if not is_test:
        args.append("--skip-tests")

    ctx.actions.run(
        executable = ctx.executable._golangci_sarif,
        inputs = srcs + dep_files + sibling_srcs + [
            ctx.file._go_mod,
            ctx.file._go_sum,
        ] + _lint_config_files(ctx),
        outputs = [output],
        arguments = args,
        mnemonic = "GavelGoGolangCILintSARIF",
        progress_message = "Generating golangci-lint submission for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        tools = [ctx.file._golangci_lint],
        use_default_shell_env = True,
    )

    return [
        _submission_output_groups(output, transitive),
        GavelGoSrcInfo(srcs = srcs, package = ctx.label.package),
    ]

go_golangci_lint_submission_aspect = aspect(
    implementation = _go_golangci_lint_aspect_impl,
    attr_aspects = [
        "deps",
        "embed",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_go_mod": attr.label(
            default = Label("//:go.mod"),
            allow_single_file = True,
        ),
        "_go_sum": attr.label(
            default = Label("//:go.sum"),
            allow_single_file = True,
        ),
        "_golangci_sarif": attr.label(
            default = Label("//lint/lang/go/golangci_lint/wrapper"),
            executable = True,
            cfg = "exec",
        ),
        "_golangci_lint": attr.label(
            default = Label("@golangci_lint//:golangci-lint"),
            allow_single_file = True,
            cfg = "exec",
        ),
    },
)

def _java_pmd_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if JavaInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_java_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".pmd.sarif")
    ctx.actions.run(
        executable = ctx.executable._pmd_sarif,
        inputs = srcs + _lint_config_files(ctx),
        outputs = [output],
        arguments = [
            "--pmd",
            ctx.file._pmd.path,
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelJavaPMDSARIF",
        progress_message = "Generating PMD submission for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        tools = [ctx.file._pmd],
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

java_pmd_submission_aspect = aspect(
    implementation = _java_pmd_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_pmd": attr.label(
            default = Label("@pmd//:bin/pmd"),
            allow_single_file = True,
            cfg = "exec",
        ),
        "_pmd_sarif": attr.label(
            default = Label("//lint/lang/java/pmd/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _java_cpd_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if JavaInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_java_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".cpd.sarif")
    ctx.actions.run(
        executable = ctx.executable._cpd_sarif,
        inputs = srcs + _lint_config_files(ctx),
        outputs = [output],
        arguments = [
            "--pmd",
            ctx.file._pmd.path,
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelJavaCPDSARIF",
        progress_message = "Generating CPD submission for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        tools = [ctx.file._pmd],
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

java_cpd_submission_aspect = aspect(
    implementation = _java_cpd_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_pmd": attr.label(
            default = Label("@pmd//:bin/pmd"),
            allow_single_file = True,
            cfg = "exec",
        ),
        "_cpd_sarif": attr.label(
            default = Label("//lint/lang/java/cpd/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _java_spotbugs_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if JavaInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    jars = _java_runtime_output_jars(target)
    if not jars:
        return [_empty_output_groups(transitive)]

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".spotbugs.sarif")
    ctx.actions.run(
        executable = ctx.executable._spotbugs_sarif,
        inputs = jars + _lint_config_files(ctx),
        outputs = [output],
        arguments = [
            "--spotbugs",
            ctx.file._spotbugs.path,
            "--out",
            output.path,
        ] + [jar.path for jar in jars],
        mnemonic = "GavelJavaSpotBugsSARIF",
        progress_message = "Generating SpotBugs submission for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        tools = [ctx.file._spotbugs],
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

java_spotbugs_submission_aspect = aspect(
    implementation = _java_spotbugs_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_spotbugs": attr.label(
            default = Label("@spotbugs//:bin/spotbugs"),
            allow_single_file = True,
            cfg = "exec",
        ),
        "_spotbugs_sarif": attr.label(
            default = Label("//lint/lang/java/spotbugs/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _python_pycompile_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if PyInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_python_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".pycompile.sarif")
    ctx.actions.run(
        executable = ctx.executable._python_pycompile_sarif,
        inputs = srcs + _lint_config_files(ctx),
        outputs = [output],
        arguments = [
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelPythonPyCompileSARIF",
        progress_message = "Generating Python py_compile submission for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

python_pycompile_submission_aspect = aspect(
    implementation = _python_pycompile_aspect_impl,
    attr_aspects = [
        "deps",
    ],
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
        execution_requirements = {"no-sandbox": "1"},
        tools = [ctx.file._ruff],
        use_default_shell_env = True,
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
        execution_requirements = {"no-sandbox": "1"},
        use_default_shell_env = True,
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

def _java_error_prone_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if JavaInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_java_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    classpath_jars = target[JavaInfo].transitive_compile_time_jars.to_list()

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".errorprone.sarif")

    classpath_str = ":".join([jar.path for jar in classpath_jars])

    args = [
        "--error-prone-jar",
        ctx.file._error_prone_jar.path,
        "--dataflow-jar",
        ctx.file._dataflow_jar.path,
        "--out",
        output.path,
    ]
    if classpath_str:
        args.extend(["--classpath", classpath_str])
    args.extend([src.path for src in srcs])

    ctx.actions.run(
        executable = ctx.executable._error_prone_sarif,
        inputs = srcs + classpath_jars + [ctx.file._error_prone_jar, ctx.file._dataflow_jar] + _lint_config_files(ctx),
        outputs = [output],
        arguments = args,
        mnemonic = "GavelJavaErrorProneSARIF",
        progress_message = "Generating Error Prone submission for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

java_error_prone_submission_aspect = aspect(
    implementation = _java_error_prone_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_error_prone_jar": attr.label(
            default = Label("@error_prone//:error_prone.jar"),
            allow_single_file = True,
            cfg = "exec",
        ),
        "_dataflow_jar": attr.label(
            default = Label("@error_prone//:dataflow_errorprone.jar"),
            allow_single_file = True,
            cfg = "exec",
        ),
        "_error_prone_sarif": attr.label(
            default = Label("//lint/lang/java/error_prone/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
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

def _go_archtest_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if GoLibrary not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]
    if "/node_modules/" in ctx.label.package:
        return [_empty_output_groups(transitive)]

    srcs = _collect_srcs(ctx)
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
        mnemonic = "GavelGoArchTest",
        progress_message = "Checking Go architecture for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

go_archtest_submission_aspect = aspect(
    implementation = _go_archtest_aspect_impl,
    attr_aspects = [
        "deps",
        "embed",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_archtest_wrapper": attr.label(
            default = Label("//lint/lang/go/archtest/wrapper"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _java_archtest_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if JavaInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_java_srcs(ctx)
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
        mnemonic = "GavelJavaArchTest",
        progress_message = "Checking Java architecture for %s" % ctx.label,
        execution_requirements = {"no-sandbox": "1"},
        use_default_shell_env = True,
    )

    return [_submission_output_groups(output, transitive)]

java_archtest_submission_aspect = aspect(
    implementation = _java_archtest_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    attrs = {
        "_lint_config": attr.label(default = Label("@@//:gavel_lint_config")),
        "_archtest_wrapper": attr.label(
            default = Label("//lint/lang/java/archtest/wrapper"),
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
        execution_requirements = {"no-sandbox": "1"},
        use_default_shell_env = True,
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

def _rust_archtest_aspect_impl(target, ctx):
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
        execution_requirements = {"no-sandbox": "1"},
        use_default_shell_env = True,
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
