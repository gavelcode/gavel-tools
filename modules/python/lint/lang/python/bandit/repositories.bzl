_VERSION = "1.9.4"

def _bandit_binary_impl(repository_ctx):
    version = repository_ctx.attr.version
    python3 = repository_ctx.which("python3")
    if not python3:
        fail("python3 not found in PATH")

    result = repository_ctx.execute(
        [
            python3,
            "-m",
            "pip",
            "install",
            "--target",
            str(repository_ctx.path("site-packages")),
            "--quiet",
            "--disable-pip-version-check",
            "bandit[sarif]==%s" % version,
        ],
        timeout = 120,
    )
    if result.return_code != 0:
        fail("pip install bandit failed: %s\n%s" % (result.stdout, result.stderr))

    repository_ctx.file(
        "BUILD.bazel",
        """
filegroup(
    name = "site_packages",
    srcs = glob(["site-packages/**"]),
    visibility = ["//visibility:public"],
)
""",
    )

bandit_binary = repository_rule(
    implementation = _bandit_binary_impl,
    attrs = {
        "version": attr.string(default = _VERSION),
    },
)
