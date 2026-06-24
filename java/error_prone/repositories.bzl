_EP_VERSION = "2.49.0"
_EP_SHA256 = "0b5a5cbf2b0c99137dcd654e9cf38d3abd9a9aaac1e7e58b059d0a5215f5196b"

_DATAFLOW_VERSION = "4.0.0"
_DATAFLOW_SHA256 = "e2c79dbc93b465b1d3f1f8ceccdd686ec44ca167f851e2fbd0fc1eb30b19803a"

def _error_prone_binary_impl(repository_ctx):
    ep_version = repository_ctx.attr.version
    repository_ctx.download(
        url = "https://repo1.maven.org/maven2/com/google/errorprone/error_prone_core/%s/error_prone_core-%s-with-dependencies.jar" % (ep_version, ep_version),
        output = "error_prone.jar",
        sha256 = repository_ctx.attr.sha256,
    )
    df_version = repository_ctx.attr.dataflow_version
    repository_ctx.download(
        url = "https://repo1.maven.org/maven2/org/checkerframework/dataflow-errorprone/%s/dataflow-errorprone-%s.jar" % (df_version, df_version),
        output = "dataflow_errorprone.jar",
        sha256 = repository_ctx.attr.dataflow_sha256,
    )
    repository_ctx.file(
        "BUILD.bazel",
        """
exports_files(["error_prone.jar", "dataflow_errorprone.jar"], visibility = ["//visibility:public"])
""",
    )

error_prone_binary = repository_rule(
    implementation = _error_prone_binary_impl,
    attrs = {
        "sha256": attr.string(default = _EP_SHA256),
        "version": attr.string(default = _EP_VERSION),
        "dataflow_sha256": attr.string(default = _DATAFLOW_SHA256),
        "dataflow_version": attr.string(default = _DATAFLOW_VERSION),
    },
)
