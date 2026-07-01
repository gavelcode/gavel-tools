# gavel-tools docs

* [Repository layout](repository-layout.md) - The lint/ + macros/ structure, the resulting labels, and tool-binary ownership.
* [The hermetic analyzer driver](tier-model.md) - Why every analyzer runs sandboxed, and the golangci-lint maintenance contract.
* [The SARIF boundary](sarif-boundary.md) - How findings flow from aspects to the platform — SARIF files on disk.
* [The catalog — two-layer config](catalog.md) - catalog.yaml is the default menu of language→tools; gavel.yaml selects per project.
* [web_project macro](web-project.md) - One macro that generates a frontend app's whole Bazel build graph.
* [Releasing](releasing.md) - One command tags a version and publishes it to the gavel registry.
* [Roadmap](roadmap.md) - What is not yet built; shipped state lives in the code and the published versions.
