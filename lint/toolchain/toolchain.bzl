"""Toolchain rule providing the `gavel-lint` multi-call helper binary."""

def _gavel_lint_toolchain_impl(ctx):
    binary = ctx.file.binary
    return [
        DefaultInfo(files = depset([binary]), runfiles = ctx.runfiles(files = [binary])),
        platform_common.ToolchainInfo(gavel_lint = binary),
    ]

gavel_lint_toolchain = rule(
    implementation = _gavel_lint_toolchain_impl,
    doc = "Wraps one gavel-lint executable for toolchain resolution.",
    attrs = {
        "binary": attr.label(
            doc = "The gavel-lint executable for the execution platform.",
            mandatory = True,
            allow_single_file = True,
            executable = True,
            cfg = "exec",
        ),
    },
)
