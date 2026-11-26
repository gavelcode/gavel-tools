"""Public bridge API: write a Bazel aspect for a linter gavel-tools does not ship.

A *bridge* is an aspect, owned by the project, that turns a tool's findings into
SARIF and returns it in the `gavel_submissions` output group. gavel builds the
aspect over the project's targets itself (fresh, cached, scoped) and reports its
findings as provided by the project, not guaranteed by gavel. The SARIF the
aspect writes must satisfy the contract in docs/bridge.md.

Stability: every symbol below is public API under the module's semver. While the
module is alpha (see README) a breaking change lands only in a minor or major
release, never a patch. Anything not exported here (common.bzl, defs.bzl, the
per-language files) is internal and may change at any time.

Minimal use:

    load("@gavel_tools//lint:bridge.bzl", "GAVEL_ATTR_ASPECTS", "gavel_forward", "gavel_submission")

    def _impl(target, ctx):
        if ctx.rule.kind != "my_library":
            return gavel_forward(ctx)
        sarif = ctx.actions.declare_file(ctx.label.name + ".mytool.sarif")
        ctx.actions.run(...)  # writes SARIF to `sarif`
        return gavel_submission(ctx, sarif)

    my_aspect = aspect(implementation = _impl, attr_aspects = GAVEL_ATTR_ASPECTS)
"""

load(
    "//lint/aspects:common.bzl",
    _collect_dep_submissions = "collect_dep_submissions",
    _empty_output_groups = "empty_output_groups",
    _safe_output_name = "safe_output_name",
    _submission_output_groups = "submission_output_groups",
)

GAVEL_SUBMISSIONS_OUTPUT_GROUP = "gavel_submissions"
"""Name of the output group gavel collects. Never change it in a bridge."""

GAVEL_ATTR_ASPECTS = ["deps", "embed"]
"""Value for the aspect's `attr_aspects`.

These are the rule attributes whose dependencies' submissions gavel_submission
and gavel_forward re-expose. An aspect that does not propagate along them
reports only the targets it is applied to directly.
"""

def gavel_submission(ctx, sarif):
    """Registers `sarif` as this target's submission and forwards its dependencies'.

    Call it as the aspect's return value for a target the aspect analysed.

    Args:
        ctx: the aspect context.
        sarif: the SARIF File the aspect's action produces for this target.
            It must follow docs/bridge.md and its name must end with the
            `sarif_suffix` declared for the tool in gavel.yaml.

    Returns:
        A list of providers for the aspect to return: an OutputGroupInfo whose
        gavel_submissions holds `sarif` plus the dependencies' submissions.
    """
    return [_submission_output_groups(sarif, _collect_dep_submissions(ctx))]

def gavel_forward(ctx):
    """Keeps a target the aspect does not analyse transparent.

    An aspect must return gavel_submissions for every target it visits, or the
    findings of that target's dependencies vanish from the collection.

    Args:
        ctx: the aspect context.

    Returns:
        A list of providers for the aspect to return: an OutputGroupInfo
        forwarding only the dependencies' submissions.
    """
    return [_empty_output_groups(_collect_dep_submissions(ctx))]

def gavel_output_name(label, suffix):
    """Collision-free output file name for a target's SARIF.

    Args:
        label: the target Label (ctx.label).
        suffix: the tool's sarif_suffix, e.g. ".mytool.sarif".

    Returns:
        A slash-free file name unique per target, ending in `suffix`.
    """
    return _safe_output_name(label) + suffix
