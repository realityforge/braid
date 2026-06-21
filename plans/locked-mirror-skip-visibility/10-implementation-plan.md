# Locked Mirror Skip Visibility Implementation Plan

Status: accepted
Date: 2026-06-21

## Phase Sequence

1. Planning approval
   - Resolve Q-01.
   - Review `00-requirements.md`, this plan, and `20-task-board.yaml`.
   - Record approval before implementation.
2. Target-selection visibility
   - Implement only the Q-01-selected visibility behavior.
   - Keep no-path target selection branch/tag-only.
   - Keep explicit locked mirror behavior unchanged.
   - Track skipped locked mirrors in the same lexicographic `cfg.Paths()` order used for targets.
   - Emit the exact stdout skip note only after the no-path command completes with nil.
3. Tests and documentation
   - Add or update focused command tests for the selected behavior.
   - Update README if output or wording changes.
4. Validation
   - Run targeted command tests while iterating.
   - Run full `bazel test //...` before completion.
   - Record evidence in `20-task-board.yaml`.

## Delivery Approach

- Keep the diff small and local to `internal/command`, tests, and README unless Q-01 selects docs-only.
- Prefer a simple helper only if it removes duplicated update/sync skip-reporting code.
- Prefer a shared helper for formatting the exact skip note if both update and sync emit the same text.
- Do not alter mirror metadata or Git plumbing.

## High-Risk Areas

- Successful-command output churn
  - Impact: scripts that expect quiet successful `update` or `sync` runs may observe new output.
  - Mitigation: make output deterministic, concise, and limited to the selected no-path cases.
- Update/sync divergence
  - Impact: users could see different no-path locked-mirror behavior between commands.
  - Mitigation: share or mirror target-selection diagnostics and test both commands.
- Noise in routine workflows
  - Impact: repositories with intentionally locked mirrors could see repeated notes.
  - Mitigation: keep wording short and only emit when locked mirrors are present.

## Required Full Gate

```bash
bazel test //...
```

## Targeted Gates

```bash
bazel test //internal/command:command_test
```

## Decision Outcomes

- Q-01 is resolved: no-path `update`, no-path `sync`, and no-path `sync --pull-only` will print the exact stdout skip note after successful command completion while preserving branch/tag-only target selection.
- The user accepted the plan and requested implementation in a subagent on 2026-06-21.

## Decision Log

| id | Plan change |
| --- | --- |
| Q-01 | Add exact skip-visibility stdout for successful no-path `update`, no-path `sync`, and no-path `sync --pull-only`; order paths lexicographically; keep locked mirrors excluded before precheck and leave explicit-path locked behavior unchanged. |

## Review Fix Log

| round | change |
| --- | --- |
| 1 | Pinned exact output stream, wording, newline shape, ordering, emission timing, failure suppression, sync mode coverage, and test expectations after iterative plan review findings. |
| 2 | Added acceptance criteria for all-locked command output, conflict-output ordering, and README coverage of the new diagnostic and lexicographic ordering. |
