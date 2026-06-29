"""Shared helpers for the Gavel lint and architecture submission aspects.

Each per-language aspect file (rust.bzl, …) loads these so the submission
output-group plumbing and output naming stay identical across languages —
the one place that has to agree for `--output_groups=gavel_submissions` to
collect every language's findings in a single build.
"""

def empty_output_groups(transitive):
    """Keeps a skipped target transparent in the submission graph.

    Aspects must always return the gavel_submissions output group; a target the
    aspect does not analyze (wrong kind, external repo, no srcs) still has to
    forward its dependencies' submissions, or the findings below it vanish from
    the top-level collection.

    Args:
        transitive: gavel_submissions depsets from the target's dependencies.

    Returns:
        An OutputGroupInfo forwarding only those transitive submissions.
    """
    return OutputGroupInfo(
        gavel_submissions = depset(transitive = transitive),
    )

def submission_output_groups(output, transitive):
    """Merges this target's SARIF into the transitive submission set.

    So a single `--output_groups=gavel_submissions` query on a top-level target
    yields the whole dependency closure's findings, not just the root's.

    Args:
        output: this target's SARIF File.
        transitive: gavel_submissions depsets from the target's dependencies.

    Returns:
        An OutputGroupInfo whose gavel_submissions depset includes output.
    """
    submissions = depset(
        direct = [output],
        transitive = transitive,
    )
    return OutputGroupInfo(
        gavel_submissions = submissions,
    )

def collect_dep_submissions(ctx):
    """Propagates findings up the dependency graph.

    Re-exposing the submissions of deps (and embed, which is how a go_test pulls
    in its library) is what lets collection at one top-level target return the
    entire transitive closure instead of a single package.

    Args:
        ctx: the aspect context whose rule deps/embed are scanned.

    Returns:
        A list of the gavel_submissions depsets exposed by dependencies.
    """
    groups = []
    if hasattr(ctx.rule.attr, "deps"):
        for dep in ctx.rule.attr.deps:
            if OutputGroupInfo in dep and hasattr(dep[OutputGroupInfo], "gavel_submissions"):
                groups.append(dep[OutputGroupInfo].gavel_submissions)
    if hasattr(ctx.rule.attr, "embed"):
        for dep in ctx.rule.attr.embed:
            if OutputGroupInfo in dep and hasattr(dep[OutputGroupInfo], "gavel_submissions"):
                groups.append(dep[OutputGroupInfo].gavel_submissions)
    return groups

def lint_config_files(ctx):
    """Makes the consumer's tool config a declared action input.

    Threading //:gavel_lint_config (architecture.yml, .golangci.yml, …) onto the
    action is what keeps it cache-correct and sandbox-safe: editing the config
    re-runs the analyzer, and nothing is read from outside the sandbox.

    Args:
        ctx: the aspect context carrying the _lint_config attribute.

    Returns:
        A list of config File objects, or empty when no filegroup is attached.
    """
    if hasattr(ctx.attr, "_lint_config"):
        return ctx.attr._lint_config.files.to_list()
    return []

def safe_output_name(label):
    """Flattens a label into a collision-free, slash-free output stem.

    declare_file cannot contain "/" but package paths do, so the package is
    folded into the name to avoid both illegal filenames and cross-package
    output collisions.

    Args:
        label: the target Label the output is generated for.

    Returns:
        A string combining the package and name with slashes replaced.
    """
    return (label.package + "_" + label.name).replace("/", "_")
