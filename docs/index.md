# gavel-tools docs

* [Repository layout](repository-layout.md) - The lint/ + macros/ structure, the resulting labels, and tool-binary ownership.
* [The sandbox axis](tier-model.md) - One question decides how each analyzer runs — does it need the real build environment?
* [The SARIF boundary](sarif-boundary.md) - How findings flow from aspects to the platform — SARIF files on disk.
* [rules_lint — breadth add-on, not a substitute](rules-lint.md) - Measured — rules_lint's reviewdog SARIF drops ruleId, so it is only for tools we don't wrap.
* [The catalog — two-layer config](catalog.md) - catalog.yaml is the default menu of language→tools; gavel.yaml selects per project.
* [web_project macro](web-project.md) - One macro that generates a frontend app's whole Bazel build graph.
* [Releasing](releasing.md) - One command tags a version and publishes it to the gavel registry.
* [Roadmap](roadmap.md) - What is not yet built; shipped state lives in the code and the published versions.
