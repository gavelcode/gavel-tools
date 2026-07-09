"""Go submission aspects: golangci-lint and archtest.

golangci-lint runs **sandboxed and hermetic** — it loads the package graph
through a gavel-owned static `GOPACKAGESDRIVER` fed the `pkg.json` + export data
that rules_go's `go_pkg_info_aspect` emits, so no host `go` and no network are
needed (see docs/tier-model.md). archtest is a pure source-parsing wrapper.
"""

load("@rules_go//go:def.bzl", "GoLibrary")
load(
    "@rules_go//go/tools/gopackagesdriver:aspect.bzl",
    "GoPkgInfo",
    "go_pkg_info_aspect",
)
load(
    ":common.bzl",
    _collect_dep_submissions = "collect_dep_submissions",
    _empty_output_groups = "empty_output_groups",
    _lint_config_files = "lint_config_files",
    _safe_output_name = "safe_output_name",
    _submission_output_groups = "submission_output_groups",
)

def _collect_srcs(ctx):
    srcs = []
    if hasattr(ctx.rule.attr, "srcs"):
        for src in ctx.rule.attr.srcs:
            srcs.extend(src.files.to_list())
    return [src for src in srcs if src.extension == "go"]

def _is_go_lint_target(target, ctx):
    if GoLibrary in target:
        return True
    if ctx.rule.kind == "go_test":
        return True
    return False

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

    if GoPkgInfo not in target:
        return [_empty_output_groups(transitive)]
    pkg_info = target[GoPkgInfo]

    is_test = ctx.rule.kind == "go_test"
    go_sdk = ctx.toolchains["@rules_go//go:toolchain"].sdk

    # The static packages driver loads the build graph from these pre-built
    # pkg.json files instead of shelling out to bazel or `go list`, so the whole
    # action runs sandboxed and offline. The manifest is the driver's only entry
    # point; the export/source/stdlib files it references must be declared inputs.
    pkg_json_files = pkg_info.pkg_json_files.to_list()
    manifest_paths = [f.path for f in pkg_json_files]
    if pkg_info.stdlib_json_file:
        manifest_paths.append(pkg_info.stdlib_json_file.path)
    manifest = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".golangci.manifest")
    ctx.actions.write(manifest, "\n".join(manifest_paths) + "\n")

    output = ctx.actions.declare_file(_safe_output_name(ctx.label) + ".golangci.sarif")

    # rules_go ships the compiled stdlib as pre-built .a archives but leaves their
    # ExportFile empty in stdlib.pkg.json. The driver needs their directory to wire
    # each stdlib package to its archive, or golangci-lint cannot load export data
    # for any dependency that imports the stdlib. With export_stdlib the archives
    # arrive in go_sdk.libs under pkg/<goos>_<goarch>; the shortest lib dirname is
    # that root (nested packages like net/http.a sit beneath it).
    stdlib_libs = go_sdk.libs.to_list()
    stdlib_pkg_dir = ""
    for stdlib_lib in stdlib_libs:
        if not stdlib_pkg_dir or len(stdlib_lib.dirname) < len(stdlib_pkg_dir):
            stdlib_pkg_dir = stdlib_lib.dirname

    args = [
        "--golangci-lint",
        ctx.file._golangci_lint.path,
        "--driver",
        ctx.executable._packages_driver.path,
        "--manifest",
        manifest.path,
        "--go",
        go_sdk.go.path,
        "--stdlib-pkg-dir",
        stdlib_pkg_dir,
        "--package",
        ctx.label.package,
        "--out",
        output.path,
    ]
    if not is_test:
        args.append("--skip-tests")

    stdlib_json = [pkg_info.stdlib_json_file] if pkg_info.stdlib_json_file else []

    ctx.actions.run(
        executable = ctx.executable._golangci_sarif,
        inputs = depset(
            direct = [
                manifest,
                ctx.file._go_mod,
                ctx.file._go_sum,
                go_sdk.go,
                go_sdk.root_file,
                go_sdk.package_list,
            ] + stdlib_json + _lint_config_files(ctx),
            transitive = [
                pkg_info.pkg_json_files,
                pkg_info.export_files,
                pkg_info.compiled_go_files,
                pkg_info.stdlib_cache_dir,
                go_sdk.srcs,
                go_sdk.libs,
                go_sdk.tools,
                go_sdk.headers,
            ],
        ),
        outputs = [output],
        arguments = args,
        mnemonic = "GavelGoGolangCILintSARIF",
        progress_message = "Generating golangci-lint submission for %s" % ctx.label,
        tools = [ctx.file._golangci_lint, ctx.executable._packages_driver],
    )

    return [_submission_output_groups(output, transitive)]

go_golangci_lint_submission_aspect = aspect(
    implementation = _go_golangci_lint_aspect_impl,
    attr_aspects = [
        "deps",
        "embed",
    ],
    requires = [go_pkg_info_aspect],
    toolchains = ["@rules_go//go:toolchain"],
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
        "_packages_driver": attr.label(
            default = Label("//lint/lang/go/golangci_lint/packagesdriver"),
            executable = True,
            cfg = "exec",
        ),
    },
)

# _go_module_prefix recovers the module path from the target's importpath so the
# wrapper can strip it off imports and match them to layers. Deriving it here
# keeps the action hermetic: go.mod is never read from the sandbox, where it does
# not exist.
def _go_module_prefix(target, ctx):
    importpath = getattr(target[GoLibrary], "importpath", "")
    package = ctx.label.package
    if not importpath:
        return ""
    if not package:
        return importpath
    suffix = "/" + package
    if importpath.endswith(suffix):
        return importpath[:-len(suffix)]
    return ""

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
            "--module-prefix",
            _go_module_prefix(target, ctx),
            "--out",
            output.path,
        ] + [src.path for src in srcs],
        mnemonic = "GavelGoArchTest",
        progress_message = "Checking Go architecture for %s" % ctx.label,
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
