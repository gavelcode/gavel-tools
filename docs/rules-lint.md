---
title: rules_lint — breadth add-on, not a substitute
type: explanation
description: Measured — rules_lint's reviewdog SARIF drops ruleId, so it is only for tools we don't wrap.
---

# rules_lint — breadth add-on, not a substitute

We considered delegating the source-only tools (ruff, pmd, eslint, …) to
[aspect-build/rules_lint](https://github.com/aspect-build/rules_lint). We measured
it, and **reversed** the idea.

## The finding: rules_lint's SARIF is lossy

rules_lint runs each linter in plain-text mode and converts that text to SARIF via
`reviewdog/errorformat`. For ruff (and similar), the rule code (`E501`) ends up
inside the **message text**, and the SARIF `ruleId` is left **empty**:

```jsonc
// ruff native (our wrapper)            // rules_lint via reviewdog
{ "ruleId": "E501", "level": "error",   { "ruleId": "",  "level": "error",
  "message": { "text": "Line too long" }}  "message": { "text": "E501 Line too long" }}
```

Gavel's parser **rejects** results with an empty `ruleId` (it keys
baselines / fingerprints / severity on it). rules_lint itself documents that it
would prefer the tool's native SARIF but cannot use it (e.g. ruff's native SARIF
prints absolute paths, [astral-sh/ruff#14985](https://github.com/astral-sh/ruff/issues/14985)) —
a problem Gavel already solves, because its parser normalizes paths.

## The conclusion

- **Native wrappers stay.** For every tool we wrap, our native-SARIF wrapper is
  *higher* fidelity than rules_lint's reviewdog output. Nothing is
  delegated-and-deleted. SpotBugs stays native too.
- **rules_lint is breadth-only.** It is worth wiring **only** for tools/languages
  we do not wrap — flake8, pylint, checkstyle, clang_tidy, cppcheck, ktlint,
  vale, yamllint, … — where reviewdog-quality findings beat *no* coverage. There,
  the lower fidelity is an acceptable trade for breadth.

So rules_lint complements gavel-tools; it does not replace any of it.
