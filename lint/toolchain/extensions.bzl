"""Module extension wiring the gavel-lint toolchain; see repositories.bzl."""

load(":repositories.bzl", "declare_gavel_lint_repos")

def _impl(mctx):
    prebuilt = False
    for mod in mctx.modules:
        for tag in mod.tags.configure:
            prebuilt = prebuilt or tag.prebuilt
    declare_gavel_lint_repos(prebuilt)
    return mctx.extension_metadata(reproducible = True)

gavel_lint = module_extension(
    implementation = _impl,
    tag_classes = {
        "configure": tag_class(
            attrs = {
                "prebuilt": attr.bool(
                    doc = "Download the prebuilt release binaries instead of building from source.",
                    mandatory = True,
                ),
            },
        ),
    },
)
