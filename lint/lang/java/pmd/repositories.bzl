_VERSION = "7.24.0"
_SHA256 = "110934b36d39c19094d1b77386931978093f238f2c2f1851748822b69c7367ac"

def _pmd_binary_impl(repository_ctx):
    version = repository_ctx.attr.version
    repository_ctx.download_and_extract(
        url = "https://github.com/pmd/pmd/releases/download/pmd_releases/%s/pmd-dist-%s-bin.zip" % (version, version),
        sha256 = repository_ctx.attr.sha256,
        stripPrefix = "pmd-bin-%s" % version,
    )
    repository_ctx.file(
        "BUILD.bazel",
        """
exports_files(glob(["**"]), visibility = ["//visibility:public"])
""",
    )

pmd_binary = repository_rule(
    implementation = _pmd_binary_impl,
    attrs = {
        "sha256": attr.string(default = _SHA256),
        "version": attr.string(default = _VERSION),
    },
)
