# Implementation Issues

Status: active
Last updated: 2026-06-14

## PI-01: Bzlmod rules_go Platform Label

- status: resolved
- discovered_in: T02
- context: The accepted plan listed full-gate cross-build platforms under `@io_bazel_rules_go//go/toolchain:*`.
- evidence:
  - `bazel build --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64 //cmd/braid:braid` failed because no repository named `@io_bazel_rules_go` is visible from the Bzlmod root.
  - `bazel build --platforms=@rules_go//go/toolchain:linux_amd64 //cmd/braid:braid` passed.
- why_it_matters: The required full-gate commands must be executable exactly as recorded in the plan and task board.
- response: Updated the active plan full-gate commands and release target labels to the Bzlmod-visible `@rules_go//go/toolchain:*` form.
- tracking_task_ids: T02
