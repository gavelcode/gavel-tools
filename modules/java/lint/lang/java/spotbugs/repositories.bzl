_VERSION = "4.9.8"
_SHA256 = "2eb8e0f2b223c22ffa2ce0c1cf1be4127dde19d240b8f7ce69a5fd3ad5c36ff3"

def _spotbugs_binary_impl(repository_ctx):
    version = repository_ctx.attr.version
    repository_ctx.download_and_extract(
        url = "https://github.com/spotbugs/spotbugs/releases/download/%s/spotbugs-%s.tgz" % (version, version),
        sha256 = repository_ctx.attr.sha256,
        stripPrefix = "spotbugs-%s" % version,
    )
    repository_ctx.file(
        "BUILD.bazel",
        """
exports_files(glob(["**"]), visibility = ["//visibility:public"])
""",
    )

spotbugs_binary = repository_rule(
    implementation = _spotbugs_binary_impl,
    attrs = {
        "sha256": attr.string(default = _SHA256),
        "version": attr.string(default = _VERSION),
    },
)
