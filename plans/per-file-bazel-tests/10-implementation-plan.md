# Per-File Bazel Test Targets Implementation Plan

Status: accepted
Date: 2026-06-22

## Phase Sequence

1. Planning review
   - Resolve open scope decisions from `00-requirements.md`.
   - Ask the user one question at a time where repo evidence does not decide the
     answer.
   - Do not mark the plan accepted until review feedback is incorporated.
2. Command test helper extraction
   - Move shared helpers from feature-specific command test files into
     helper-only `_test.go` sources.
   - Keep helper code in package `command` so tests retain access to unexported
     command internals.
   - Put shared helper code behind a test-only support `go_library` that embeds
     `:command`, matching the integration package's support-library shape while
     keeping the helper file named `*_test.go` for normal Go tooling.
3. Command package BUILD split
   - Replace `//internal/command:command_test` with one `go_test` per
     `internal/command/*_test.go` feature file that contains runnable tests.
   - Do not create standalone `go_test` labels for helper-only `_test.go`
     support sources.
   - Have command per-file tests embed the test-only support library instead of
     repeating helper sources directly in each target.
   - Preserve `timeout = "long"` on command per-file targets unless focused
     per-file timing proves a smaller timeout is safe.
   - Run focused per-file command tests and the package pattern.
4. Integration BUILD split
   - Remove `//integration:braid_integration_test`.
   - Remove `manual` tags from per-file integration test targets.
   - Keep the existing `test_support` library because it already matches the
     package pattern and avoids duplicating integration helpers.
   - Keep `size = "medium"` on integration per-file targets.
   - Run `bazel test //integration/...`.
5. CI and documentation alignment
   - Update `.github/workflows/ci.yml` aggregate labels to Bazel patterns.
   - Update `AGENTS.md` CI parity notes to match the workflow's required
     commands.
   - Update current developer-facing docs that mention removed aggregate
     targets, including `AGENTS.md` and README/developer docs if matched.
   - Do not edit historical plan artifacts solely to rewrite old aggregate
     labels.
6. Full validation
   - Re-read `.github/workflows/ci.yml` after CI edits and use that final file
     as the source of truth for the full gate.
   - Run the required local CI parity gates.
   - Confirm old aggregate targets are gone with `bazel query`.
   - Confirm focused per-file labels work.
   - Run `git diff --exit-code` only after formatting and all planned edits are
     complete; before the final commit this may require staging or committing
     first, so record the exact timing in task evidence.

## Delivery Approach

- Keep the change limited to Bazel target structure, test helper movement, CI,
  and local developer instructions.
- Preserve test behavior; a test file should still contain the same `Test*`
  functions after the split.
- Prefer obvious file-to-label naming: `foo_test.go` becomes `:foo_test`.
- Remove compatibility aggregates rather than preserving aliases.
- Use target patterns in CI so adding a future test file requires only a BUILD
  target, not a CI label update.

## High-Risk Areas

- Same-package helper access
  - Impact: moving helpers to a separate package would lose access to unexported
    command types and functions.
  - Mitigation: keep command helpers in package `command` as test-only sources.
- Helper source inclusion
  - Impact: a per-file target may fail to compile if a helper source is omitted.
  - Mitigation: embed a shared test-only support library from each per-file test
    target and build each per-file target explicitly after the split.
- Support library expectations
  - Impact: `rules_go` embeds same-package sources into internal test archives,
    so the support library matches the integration package pattern but does not
    make white-box test helpers behave like a separately linked external
    dependency.
  - Mitigation: keep the support-library shape for BUILD maintainability and
    normal Go tooling hygiene; use Bazel action evidence to document the
    remaining same-package internal-test compilation behavior.
- Duplicate test execution
  - Impact: keeping aggregates while removing `manual` tags would run the same
    tests more than once under `bazel test //...`.
  - Mitigation: remove aggregate targets for converted packages.
- CI/platform runtime
  - Impact: using `bazel test //...` in every matrix leg would run non-
    integration tests repeatedly on all platforms.
  - Mitigation: keep Go quality on `bazel test //...` and use
    `bazel test //integration/...` in the platform matrix unless user review
    chooses a broader matrix command.
- Developer muscle memory
  - Impact: existing aggregate labels fail after removal.
  - Mitigation: update `AGENTS.md` and expose new focused labels in the plan.
- Timeout regression
  - Impact: large command test files could fail under Bazel's default timeout
    after losing the aggregate target's `timeout = "long"`.
  - Mitigation: preserve `timeout = "long"` on command per-file targets unless
    focused per-file timing supports a narrower value.

## Required Full Gate

The source of truth is `.github/workflows/ci.yml`. After implementation, run the
same checks locally:

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
bazel test //integration/...
```

## Targeted Gates

```bash
bazel query 'kind("go_test rule", //internal/command:*)'
bazel test //internal/command:add_test
bazel test //internal/command:push_test
bazel test //internal/command:sync_test
bazel test //internal/command/...
bazel test //...
```

```bash
bazel query 'kind("go_test rule", //integration:*)'
bazel test //integration:lifecycle_test
bazel test //integration/...
```

## Decision Outcomes

- Q-01 is resolved.
- The plan converts both `internal/command` and `integration`.
- One-file test packages are left unchanged.
- The plan was accepted by the user on 2026-06-22 by requesting iterative plan
  review followed by implementation.

## Decision Log

| id | Plan change |
| --- | --- |
| Q-01 | Convert both `internal/command` and `integration`; leave one-file packages alone. `integration/BUILD.bazel` loses `braid_integration_test` and `manual` tags, and the integration CI matrix switches to `bazel test //integration/...`. |

## Implementation Tasks

Tasks are tracked in `20-task-board.yaml`.

- `PLAN-QUESTIONS`: resolve grill-me questions and update plan artifacts.
- `PLAN-APPROVAL`: request and record user approval of the final plan.
- `T01`: extract command test helpers.
- `T02`: split command package test targets.
- `T03`: split integration test targets.
- `T04`: update CI and developer parity notes.
- `T05`: run full validation and record evidence.

## Completion Criteria

- No open questions remain.
- User review outcome is recorded before status changes to accepted.
- Converted packages have no aggregate Go test targets.
- Converted per-file targets have no `manual` tag.
- Every converted feature test file with runnable tests has a matching
  `go_test` label.
- Helper-only `_test.go` support sources are included by needed targets and do
  not require standalone labels.
- `bazel test //...` passes.
- CI workflow and `AGENTS.md` required checks match.
- Task evidence is recorded.

## Plan Review Finding Log

| round | finding | assessment | plan change |
| --- | --- | --- | --- |
| R1 | Accepted state lacked review-loop evidence. | Valid. | Added review feedback evidence and history in `20-task-board.yaml`; plan remains accepted because the user explicitly requested review followed by implementation. |
| R1 | Helper-only `_test.go` target rule was ambiguous. | Valid. | Clarified that only feature test files with runnable tests receive `go_test` labels; helper-only test files are shared sources. |
| R1 | Command timeout policy was missing. | Valid. | Required command per-file targets to preserve `timeout = "long"` unless focused timing justifies narrowing. |
| R1 | Documentation scope could rewrite historical plans. | Valid. | Limited docs updates to current developer-facing docs and explicitly excluded historical plan rewrites. |
| R1 | Final CI parity timing was underspecified. | Valid. | Required re-reading `.github/workflows/ci.yml` after edits and recording exact `git diff --exit-code` timing. |
| Post-commit | Command test support was repeated as raw source in every test target. | Valid. | Switched `internal/command` to a test-only `command_test_support` `go_library` embedded by each per-file `go_test`, while keeping the helper file as `_test.go` to avoid normal Go tooling pollution. |
