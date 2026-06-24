_VERSION = "0.15.12"

_SHA256 = {
    "x86_64-apple-darwin": "f88b6ad8338ad7acf83497e4bc2f2b5d3cac46c3e71363b7263c1bb9027c9b37",
    "aarch64-apple-darwin": "bd59107e125f9c9f64a75be13b6ff21bf7cbc61c4b9529bbb75c8184e3dd6c3c",
    "x86_64-unknown-linux-gnu": "5e26a7811f7db364864ace00cced53003556a37b63f3c987e340b18207776f1c",
    "aarch64-unknown-linux-gnu": "4dcd4cd8beb525d04294eeab67d84d199cce2c44b66858978ca729b694fe29ff",
}

def _ruff_binary_impl(repository_ctx):
    version = repository_ctx.attr.version
    platform = _platform(repository_ctx)
    if platform not in _SHA256:
        fail("unsupported ruff host platform: %s" % platform)

    archive = "ruff-%s.tar.gz" % platform
    repository_ctx.download_and_extract(
        url = "https://github.com/astral-sh/ruff/releases/download/%s/%s" % (version, archive),
        sha256 = _SHA256[platform],
        stripPrefix = "ruff-%s" % platform,
    )
    repository_ctx.file(
        "BUILD.bazel",
        """
exports_files(["ruff"], visibility = ["//visibility:public"])
""",
    )

def _platform(repository_ctx):
    os_name = repository_ctx.os.name.lower()
    arch = repository_ctx.os.arch.lower()

    if os_name.startswith("mac os"):
        os_name = "apple-darwin"
    elif os_name.startswith("linux"):
        os_name = "unknown-linux-gnu"

    if arch in ["x86_64", "amd64"]:
        arch = "x86_64"
    elif arch in ["aarch64", "arm64"]:
        arch = "aarch64"

    return "%s-%s" % (arch, os_name)

ruff_binary = repository_rule(
    implementation = _ruff_binary_impl,
    attrs = {
        "version": attr.string(default = _VERSION),
    },
)
