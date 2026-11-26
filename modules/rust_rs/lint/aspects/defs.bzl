# Public aspects of this module, bound to rules_rs's Rust ruleset. The logic is
# shared with core: only the upstream Clippy aspect differs.
load("@gavel_tools//lint/aspects:rust_clippy.bzl", "make_rust_clippy_submission_aspect")
load("@rules_rust//rust:defs.bzl", _rust_clippy_aspect_upstream = "rust_clippy_aspect")

rust_clippy_submission_aspect = make_rust_clippy_submission_aspect(_rust_clippy_aspect_upstream)
