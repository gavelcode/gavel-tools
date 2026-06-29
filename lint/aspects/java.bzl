"""Java/Kotlin submission aspects: PMD, CPD, SpotBugs, Error Prone and archtest.

archtest is a pure source-parsing wrapper and runs sandboxed. The four analyzers
are JVM tools; they are hermetized one by one by threading the Bazel JDK
toolchain onto each action (see the per-aspect notes) instead of relying on a
host java/JAVA_HOME.
"""

load("@rules_java//java/common:java_info.bzl", "JavaInfo")
load(
    ":common.bzl",
    _collect_dep_submissions = "collect_dep_submissions",
    _empty_output_groups = "empty_output_groups",
    _lint_config_files = "lint_config_files",
    _safe_output_name = "safe_output_name",
    _submission_output_groups = "submission_output_groups",
)

def _collect_java_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension == "java"]

def _java_runtime_output_jars(target):
    return [jar for jar in target[JavaInfo].runtime_output_jars if jar.extension == "jar"]

def _jvm_env(java_runtime):
    # Run the analyzer under the toolchain JDK (hermetic) while still giving the
    # tool's shell launcher a PATH to find coreutils (dirname, readlink, …).
    return {
        "JAVA_HOME": java_runtime.java_home,
        "PATH": "/usr/bin:/bin",
    }

def _java_pmd_aspect_impl(target, ctx):
    transitive = _collect_dep_submissions(ctx)
    if JavaInfo not in target:
        return [_empty_output_groups(transitive)]
    if ctx.label.workspace_name:
        return [_empty_output_groups(transitive)]

    srcs = _collect_java_srcs(ctx)
    if not srcs:
        return [_empty_output_groups(transitive)]

    java_runtime = ctx.toolchains["@bazel_tools//tools/jdk:runtime_toolchain_type"].java_runtime

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".pmd.sarif")
    ctx.actions.run(
        executable = ctx.executable._pmd_sarif,
        inputs = depset(
            direct = srcs + _lint_config_files(ctx),
            transitive = [java_runtime.files],
        ),
        outputs = [output],
        arguments = [
            "--pmd",
            ctx.file._pmd.path,
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelJavaPMDSARIF",
        progress_message = "Generating PMD submission for %s" % ctx.label,
        env = _jvm_env(java_runtime),
        tools = [ctx.file._pmd],
    )

    return [_submission_output_groups(output, transitive)]

java_pmd_submission_aspect = aspect(
    implementation = _java_pmd_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    toolchains = ["@bazel_tools//tools/jdk:runtime_toolchain_type"],
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

    java_runtime = ctx.toolchains["@bazel_tools//tools/jdk:runtime_toolchain_type"].java_runtime

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".cpd.sarif")
    ctx.actions.run(
        executable = ctx.executable._cpd_sarif,
        inputs = depset(
            direct = srcs + _lint_config_files(ctx),
            transitive = [java_runtime.files],
        ),
        outputs = [output],
        arguments = [
            "--pmd",
            ctx.file._pmd.path,
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelJavaCPDSARIF",
        progress_message = "Generating CPD submission for %s" % ctx.label,
        env = _jvm_env(java_runtime),
        tools = [ctx.file._pmd],
    )

    return [_submission_output_groups(output, transitive)]

java_cpd_submission_aspect = aspect(
    implementation = _java_cpd_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    toolchains = ["@bazel_tools//tools/jdk:runtime_toolchain_type"],
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

    java_runtime = ctx.toolchains["@bazel_tools//tools/jdk:runtime_toolchain_type"].java_runtime

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".spotbugs.sarif")
    ctx.actions.run(
        executable = ctx.executable._spotbugs_sarif,
        inputs = depset(
            direct = jars + _lint_config_files(ctx),
            transitive = [java_runtime.files],
        ),
        outputs = [output],
        arguments = [
            "--spotbugs",
            ctx.file._spotbugs.path,
            "--out",
            output.path,
        ] + [jar.path for jar in jars],
        mnemonic = "GavelJavaSpotBugsSARIF",
        progress_message = "Generating SpotBugs submission for %s" % ctx.label,
        env = _jvm_env(java_runtime),
        tools = [ctx.file._spotbugs],
    )

    return [_submission_output_groups(output, transitive)]

java_spotbugs_submission_aspect = aspect(
    implementation = _java_spotbugs_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    toolchains = ["@bazel_tools//tools/jdk:runtime_toolchain_type"],
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

    # Use the classpath Bazel actually compiled this target with, not
    # transitive_compile_time_jars: the latter is "what others compile against
    # this target" and is empty for leaf targets like java_test, so Error Prone
    # would see neither JUnit nor the target's own deps and report every symbol
    # as missing. compilation_info.compilation_classpath is the real javac path.
    compilation = target[JavaInfo].compilation_info
    if compilation:
        classpath_jars = compilation.compilation_classpath.to_list()
    else:
        classpath_jars = target[JavaInfo].transitive_compile_time_jars.to_list()
    java_runtime = ctx.toolchains["@bazel_tools//tools/jdk:runtime_toolchain_type"].java_runtime

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".errorprone.sarif")

    classpath_str = ":".join([jar.path for jar in classpath_jars])

    args = [
        "--error-prone-jar",
        ctx.file._error_prone_jar.path,
        "--dataflow-jar",
        ctx.file._dataflow_jar.path,
        "--javac",
        java_runtime.java_home + "/bin/javac",
        "--out",
        output.path,
    ]
    if classpath_str:
        args.extend(["--classpath", classpath_str])
    args.extend([src.path for src in srcs])

    ctx.actions.run(
        executable = ctx.executable._error_prone_sarif,
        inputs = depset(
            direct = srcs + classpath_jars + [ctx.file._error_prone_jar, ctx.file._dataflow_jar] + _lint_config_files(ctx),
            transitive = [java_runtime.files],
        ),
        outputs = [output],
        arguments = args,
        mnemonic = "GavelJavaErrorProneSARIF",
        progress_message = "Generating Error Prone submission for %s" % ctx.label,
        env = _jvm_env(java_runtime),
    )

    return [_submission_output_groups(output, transitive)]

java_error_prone_submission_aspect = aspect(
    implementation = _java_error_prone_aspect_impl,
    attr_aspects = [
        "deps",
    ],
    toolchains = ["@bazel_tools//tools/jdk:runtime_toolchain_type"],
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
