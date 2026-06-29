load("@rules_go//go:def.bzl", "GoLibrary")
load("@rules_java//java/common:java_info.bzl", "JavaInfo")
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

rust_clippy_submission_aspect = _rust_clippy_submission_aspect

rust_archtest_submission_aspect = _rust_archtest_submission_aspect

python_pycompile_submission_aspect = _python_pycompile_submission_aspect

python_ruff_submission_aspect = _python_ruff_submission_aspect

python_bandit_submission_aspect = _python_bandit_submission_aspect

python_archtest_submission_aspect = _python_archtest_submission_aspect

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

def _collect_typescript_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension in ("ts", "tsx")]

def _java_runtime_output_jars(target):
    return [jar for jar in target[JavaInfo].runtime_output_jars if jar.extension == "jar"]

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
