_VERSION = "2.11.4"

_SHA256 = {
    "darwin-amd64": "c900d4048db75d1edfd550fd11cf6a9b3008e7caa8e119fcddbc700412d63e60",
    "darwin-arm64": "02db2a2dae8b26812e53b0688a6f617e3ef1f489790e829ea22862cf76945675",
    "linux-amd64": "200c5b7503f67b59a6743ccf32133026c174e272b930ee79aa2aa6f37aca7ef1",
    "linux-arm64": "3bcfa2e6f3d32b2bf5cd75eaa876447507025e0303698633f722a05331988db4",
}

def _golangci_lint_binary_impl(repository_ctx):
    version = repository_ctx.attr.version
    platform = _platform(repository_ctx)
    if platform not in _SHA256:
        fail("unsupported golangci-lint host platform: %s" % platform)

    archive = "golangci-lint-%s-%s.tar.gz" % (version, platform)
    repository_ctx.download_and_extract(
        url = "https://github.com/golangci/golangci-lint/releases/download/v%s/%s" % (version, archive),
        sha256 = _SHA256[platform],
        stripPrefix = "golangci-lint-%s-%s" % (version, platform),
    )
    repository_ctx.file(
        "BUILD.bazel",
        """
exports_files(["golangci-lint"], visibility = ["//visibility:public"])
""",
    )

def _platform(repository_ctx):
    os_name = repository_ctx.os.name.lower()
    arch = repository_ctx.os.arch.lower()

    if os_name.startswith("mac os"):
        os_name = "darwin"
    elif os_name.startswith("linux"):
        os_name = "linux"

    if arch in ["x86_64", "amd64"]:
        arch = "amd64"
    elif arch in ["aarch64", "arm64"]:
        arch = "arm64"

    return "%s-%s" % (os_name, arch)

golangci_lint_binary = repository_rule(
    implementation = _golangci_lint_binary_impl,
    attrs = {
        "version": attr.string(default = _VERSION),
    },
)
