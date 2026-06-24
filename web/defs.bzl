"""web_project: one macro that generates the Bazel target graph for a frontend
app — bundle, styles, production dist, type-check and lint — so consumers stop
hand-wiring esbuild + copy_file + tailwind + tsc + eslint by hand.

The per-tool binaries (eslint, tsc, tailwind) come from the consumer's own npm
lockfile, so the consumer loads them and passes them via `tools`; the macro
only wires the targets. Tool versions stay owned by the consumer.

Generated targets (for `name = "web"`):

    :web             production build filegroup (dist/*)   -> bazel build
    :web.bundle      esbuild bundle (intermediate)
    :web.styles      tailwind css (intermediate, optional)
    :web.typecheck   tsc --noEmit                          -> bazel test
    :web.lint        eslint                                -> bazel test / judge

There is no runnable `bazel run` target: a frontend is served embedded by the
backend, and dev (watch/HMR) is run outside Bazel.
"""

load("@aspect_bazel_lib//lib:copy_file.bzl", "copy_file")
load("@aspect_rules_esbuild//esbuild:defs.bzl", "esbuild")

_DEFAULT_ESBUILD_CONFIG = {
    "jsx": "automatic",
    "loader": {".css": "empty"},
}

def _node_modules(names):
    return [":node_modules/" + name for name in names]

def web_project(
        name,
        entry_point,
        tsconfig,
        tools,
        srcs = None,
        html = "index.html",
        runtime_deps = [],
        type_deps = [],
        lint_deps = [],
        css_deps = [],
        css_entry = "src/index.css",
        tailwind_config = "tailwind.config.js",
        eslint_config = "eslint.config.js",
        esbuild_config = None,
        esbuild_target = "es2020",
        assets = None,
        visibility = ["//visibility:public"],
        server_visibility = None):
    """Wire a frontend app's build + checks from a single declaration.

    Args:
        name: base name; the production build is `:{name}`.
        entry_point: bundle entry, e.g. "src/main.tsx".
        tsconfig: tsconfig used for bundling, type-check and lint.
        tools: struct(eslint, tsc, tailwind) of the consumer's npm `bin` modules.
        srcs: TS/TSX/CSS sources; defaults to globbing `src/**`.
        html: HTML entry copied into the dist output.
        runtime_deps: npm package names linked for the bundle.
        type_deps: npm package names providing types (typescript, @types/*).
        lint_deps: npm package names the eslint config needs (plugins, presets).
        css_deps: npm package names for tailwind; empty disables the css step.
        css_entry: tailwind input css.
        tailwind_config: tailwind config file.
        eslint_config: eslint flat-config file.
        esbuild_config: esbuild `config` dict; defaults to JSX automatic.
        esbuild_target: esbuild `target`.
        assets: static assets; defaults to globbing `public/**`.
        visibility: visibility of the build target.
        server_visibility: extra visibility for embedding in a backend.
    """
    ts_srcs = srcs if srcs != None else native.glob(["src/**/*.ts", "src/**/*.tsx"])
    css_srcs = native.glob(["src/**/*.css"])
    static_assets = assets if assets != None else native.glob(["public/**/*"], allow_empty = True)
    config_srcs = [tsconfig, eslint_config] + ([tailwind_config] if css_deps else [])

    runtime = _node_modules(runtime_deps)
    types = _node_modules(type_deps)
    lint = _node_modules(lint_deps)
    css_pkgs = _node_modules(css_deps)

    esbuild(
        name = name + ".bundle",
        srcs = ts_srcs + css_srcs,
        config = esbuild_config if esbuild_config != None else _DEFAULT_ESBUILD_CONFIG,
        entry_point = entry_point,
        minify = True,
        output = name + ".bundle.js",
        platform = "browser",
        target = esbuild_target,
        tsconfig = tsconfig,
        deps = runtime,
    )

    dist = [
        _copy(name, "dist/index.html", html),
        _copy(name, "dist/bundle.js", name + ".bundle.js"),
        _copy(name, "dist/bundle.js.map", name + ".bundle.js.map"),
    ]

    if css_deps:
        tools.tailwind.tailwindcss(
            name = name + ".styles",
            srcs = css_srcs + ts_srcs + config_srcs + [html] + css_pkgs,
            outs = [name + ".styles.css"],
            args = ["-i", css_entry, "-o", name + ".styles.css", "--config", tailwind_config, "--minify"],
            chdir = native.package_name(),
        )
        dist.append(_copy(name, "dist/styles.css", name + ".styles.css"))

    build_vis = list(visibility) + (list(server_visibility) if server_visibility else [])
    native.filegroup(
        name = name,
        srcs = dist + static_assets,
        visibility = build_vis,
    )

    tools.tsc.tsc_test(
        name = name + ".typecheck",
        args = ["--noEmit", "-p", tsconfig],
        chdir = native.package_name(),
        data = ts_srcs + css_srcs + config_srcs + runtime + types,
    )

    tools.eslint.eslint_test(
        name = name + ".lint",
        args = ["--config", eslint_config, "src/"],
        chdir = native.package_name(),
        data = ts_srcs + config_srcs + runtime + types + lint,
    )

def _copy(name, out, src):
    target = name + "." + out.replace("/", "_").replace(".", "_")
    copy_file(name = target, src = src, out = out)
    return ":" + target
