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

## PI-02: Implementation Review Parity Evidence Gaps

- status: resolved
- discovered_in: iterative-plan-implementation-review round 1
- context: `01-command-parity.md` had completed status, but the acceptance checklist was still unchecked and the implemented test evidence did not explicitly cover all high-risk `diff` and `push` mirror variants.
- evidence:
  - Added `diff` integration coverage for tag, revision-locked, subdirectory, and path-with-spaces mirrors.
  - Added `push` integration coverage for revision-locked mirrors requiring `--branch` and pushing successfully to an explicit branch.
  - Added short production comments around temporary index composition, `merge-recursive`, temp-repo push assembly, alternates, and sparse checkout.
  - `bazel test //internal/command:command_test` passed.
  - `bazel test //internal/gitexec:gitexec_test` passed.
  - `bazel test //...` passed.
  - All required `@rules_go//go/toolchain:*` release target builds passed.
- why_it_matters: The plan promises stronger parity and maintainability evidence than a direct Ruby test port, so completed status must be backed by explicit tests and readable non-obvious Git choices.
- response: Filled the missing tests, updated the command parity checklist, clarified the revision/tag branch requirement diagnostic, and recorded review evidence in the task board.
- tracking_task_ids: T10, T14, T18
