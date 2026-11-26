"""Example bridge: a hermetic `TODO` finder, wired through @gavel_tools//lint:bridge.bzl.

Copy this to start a bridge of your own; the docs are in docs/bridge.md.
"""

load("//lint:bridge.bzl", "GAVEL_ATTR_ASPECTS", "gavel_forward", "gavel_output_name", "gavel_submission")

# One SARIF result per line containing TODO; plain awk, no network, no host tools
# beyond the shell.
_AWK = r"""
BEGIN {
  printf "{\"version\":\"2.1.0\",\"runs\":[{\"tool\":{\"driver\":{\"name\":\"todo\"}},\"results\":["
  sep = ""
}
/TODO/ {
  printf "%s{\"ruleId\":\"todo/found\",\"level\":\"warning\",\"message\":{\"text\":\"TODO found\"},", sep
  printf "\"locations\":[{\"physicalLocation\":{\"artifactLocation\":{\"uri\":\"%s\"},\"region\":{\"startLine\":%d}}}]}", FILENAME, FNR
  sep = ","
}
END {
  printf "],\"invocations\":[{\"executionSuccessful\":true}]}]}\n"
}
"""

def _aspect_impl(target, ctx):
    if ctx.label.workspace_name or ctx.rule.kind != "todo_library":
        return gavel_forward(ctx)
    srcs = [f for src in ctx.rule.attr.srcs for f in src.files.to_list() if f.is_source]
    sarif = ctx.actions.declare_file(gavel_output_name(ctx.label, ".todo.sarif"))
    ctx.actions.run_shell(
        inputs = srcs,
        outputs = [sarif],
        command = "awk '%s' \"$@\" > %s" % (_AWK, sarif.path),
        arguments = [f.path for f in srcs],
        mnemonic = "GavelTodoBridge",
        progress_message = "Looking for TODOs in %s" % ctx.label,
    )
    return gavel_submission(ctx, sarif)

todo_aspect = aspect(
    implementation = _aspect_impl,
    attr_aspects = GAVEL_ATTR_ASPECTS,
)

def _library_impl(ctx):
    return [DefaultInfo(files = depset(ctx.files.srcs))]

todo_library = rule(
    implementation = _library_impl,
    attrs = {
        "srcs": attr.label_list(allow_files = True),
        "deps": attr.label_list(),
    },
)

# A kind the aspect does not analyse, to prove skipped targets stay transparent.
todo_passthrough = rule(
    implementation = _library_impl,
    attrs = {
        "srcs": attr.label_list(allow_files = True),
        "deps": attr.label_list(),
    },
)

def _report_impl(ctx):
    return [DefaultInfo(files = depset(transitive = [
        dep[OutputGroupInfo].gavel_submissions
        for dep in ctx.attr.deps
    ]))]

# Test harness: exposes what `--output_groups=gavel_submissions` would build.
todo_report = rule(
    implementation = _report_impl,
    attrs = {"deps": attr.label_list(aspects = [todo_aspect])},
)
