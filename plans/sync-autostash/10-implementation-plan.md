# Braid Sync Autostash Implementation Plan

Status: accepted
Date: 2026-06-21

## Phase Sequence

1. Resolve open questions
   - Ask one grill-me question at a time.
   - Update `00-requirements.md`, this plan, and `20-task-board.yaml` after
     each decision.
   - Do not mark the plan accepted while any `Q-*` item is open.
2. Plan review and approval
   - Ask the user to review the latest plan after all questions are resolved.
   - Incorporate feedback.
   - Record approval outcome before implementation starts.
3. CLI surface
   - Add the selected flag to `cli.SyncOptions`.
   - Parse the selected flag for `sync`.
   - Update command usage.
   - Add parser tests.
4. Git execution plumbing
   - Add Git wrapper methods for path-scoped stash save, stash worktree apply,
     selected-path index restoration from the stash index parent, stash drop,
     and any stash-ref lookup needed to identify the saved entry.
   - Add ignored-aware selected-path status plumbing for autostash decisions.
   - Keep Git command invocation behind `internal/gitexec`.
   - Add gitexec tests using real temp repositories.
5. Sync orchestration
   - Refactor update internals to return explicit update status so sync can
     distinguish ordinary success/no-op from update conflict state.
   - Split sync precheck into immutable blockers, existing non-autostash
     selected mirror dirty handling, and ignored-aware autostash selected mirror
     dirty handling.
   - When the flag is absent, preserve current dirty selected mirror behavior.
   - When the flag is present, save dirty selected mirror path state before
     hydration, push planning, push, or update work.
   - Restore saved state according to the resolved recovery policy.
6. Command and integration coverage
   - Add command tests for success, default failure, config blocker, scoped
     selection, staged/unstaged restoration, untracked restoration, operational
     failure restoration, and conflict recovery.
   - Add executable integration coverage for at least one successful
     `sync --autostash` workflow.
7. Documentation and full gate
   - Update README sync docs and command reference.
   - Run targeted tests while iterating.
   - Run the full gate before marking implementation complete.

## Delivery Approach

- Preserve existing default `sync` behavior.
- Add the new behavior behind one opt-in flag.
- Prefer Git's native stash machinery over custom patch replay unless open
  questions reject visible stash entries.
- Keep path scoping explicit and NUL-safe.
- Keep `.braids.json` outside autostash handling.
- Avoid broad reset or checkout operations.
- Keep restoration failure recoverable rather than trying to hide it.

## High-Risk Areas

- Stash reference identification
  - Impact: dropping or applying the wrong stash would be severe.
  - Mitigation: capture the new entry's OID plus identifying message/ref
    evidence, resolve the current stash reflog selector before drop, and test
    with existing user stash entries before and after the Braid entry.
- Index restoration
  - Impact: staged context is central to the feature.
  - Mitigation: real Git tests for staged and unstaged changes to the same file,
    plus integration coverage through `braid sync`.
- Update conflict interaction
  - Impact: auto-applying saved work onto Braid conflict markers could obscure
    the update conflict.
  - Mitigation: do not auto-restore in update-conflict state; leave the stash
    intact, return a command error when an autostash exists, and test the
    recovery instructions.
- Partial update-conflict failure
  - Impact: conflict markers or config state can be written before conflict
    instruction or `MERGE_MSG` writing fails.
  - Mitigation: make update return result plus error so sync suppresses
    auto-restore whenever conflict state was written, even on later errors.
- Failure cleanup
  - Impact: sync failures after saving user work must not strand the work
    silently.
  - Mitigation: defer-style restoration with explicit saved-state diagnostics.
- Post-restore stash cleanup
  - Impact: work may be restored successfully while the stash entry cannot be
    safely dropped.
  - Mitigation: treat unresolved stash cleanup as a non-zero cleanup error,
    leave restored work and the stash intact, and print manual cleanup
    instructions.
- Ignored-only detection
  - Impact: using existing `git status --porcelain -- <path>` would miss
    ignored-only selected mirror files even though the plan promises to capture
    ignored files.
  - Mitigation: use ignored-aware selected-path status for autostash decisions
    and test ignored-only selected mirror paths.
- Unrelated index preservation
  - Impact: stash save/apply operations can affect the index, so unrelated
    staged files must be explicitly protected.
  - Mitigation: add gitexec and command tests for unrelated staged and unstaged
    files outside selected mirror paths across save, restore, and restore
    failure.
- Pathspec safety
  - Impact: mirror paths may contain spaces or shell-sensitive characters.
  - Mitigation: use `--pathspec-from-file` with NUL separation.
- Ignored file capture
  - Impact: `--all` can remove ignored generated files while sync runs.
  - Mitigation: combine `--all` with selected mirror pathspecs only; test that
    ignored files outside selected paths are untouched.
- Existing behavior regression
  - Impact: `braid sync` without the flag must keep current safety semantics.
  - Mitigation: preserve existing tests and add explicit no-flag dirty-path
    coverage.

## Required Full Gate

```bash
bazel test //...
```

## Targeted Gates

```bash
bazel test //internal/cli:cli_test
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

## Decision Outcomes

- Q-01 is resolved: the public flag is `--autostash`.
- Q-02 is resolved: `--autostash` applies to both default sync and
  `sync --pull-only`.
- Q-03 is resolved: selected mirror path state includes ignored files through
  path-scoped `git stash push --all`.
- Q-04 is resolved: autostash state uses a normal Git stash entry with a clear
  message and exact-ref tracking.
- Q-05 is resolved: one path-scoped stash is created before operational phases
  and restored once at command exit according to the recovery policy.
- Q-06 is resolved: update-conflict state leaves the stash intact and prints
  manual recovery instructions instead of auto-restoring.
- Q-07 is resolved: staged selected mirror-path changes are allowed and restored
  by applying worktree state without `--index` and then restoring only selected
  mirror paths to the index from the stash index parent.
- No behavior questions remain open.

## Implementation Issue Log

| id | status | context and evidence | response |
| --- | --- | --- | --- |
| I-01 | planned | T02 real-repo testing showed `git stash apply --index <oid>` can replay unrelated outside index state from a path-scoped stash and fail when unrelated paths have both staged and unstaged changes. | Replace whole-stash `apply --index` with `git stash apply <oid>` plus selected mirror-path index restoration from `<oid>^2`; keep staged mirror-path support and unrelated index preservation requirements. |
- Plan approved for implementation by user request to implement in a subagent on
  2026-06-21.

## Plan Review Finding Log

| round | finding | assessment | plan change |
| --- | --- | --- | --- |
| R1 | Update-conflict state has no reliable signal. | Valid. | Added explicit update result/status contract and sync conflict handling requirements. |
| R1 | Ignored-only mirror dirtiness is not detected. | Valid. | Added ignored-aware selected-path status requirement and tests. |
| R1 | Stash identity and drop semantics are underspecified. | Valid. | Specified OID/message capture plus current stash reflog selector resolution before drop. |
| R1 | Unrelated index preservation lacks explicit coverage. | Valid. | Added gitexec/command coverage for unrelated staged and unstaged state across save/restore/failure. |
| R2 | Update-conflict exit semantics remain undefined. | Valid. | Defined non-zero sync behavior, stdout/stderr split, and skipped-summary suppression when a saved stash remains after update conflict. |
| R2 | Partial conflict-state failures are not covered. | Valid. | Required update result-plus-error contract and restore suppression after conflict state is written even if later steps fail. |
| R2 | Post-restore drop failure has no return policy. | Valid. | Defined post-restore stash cleanup failure as a command error with restored work preserved and manual cleanup instructions. |
| R2 | Task board under-scopes the update refactor. | Valid. | Added update files/tests and public update behavior coverage to task scope. |

## Decision Log

| id | Plan change |
| --- | --- |
| Q-01 | Use only `--autostash` as the public flag; do not add `--auto-stash` or aliases. |
| Q-02 | Apply `--autostash` to both default `sync` and `sync --pull-only`; add pull-only coverage for dirty selected mirror restoration. |
| Q-03 | Capture ignored files under selected mirror paths by using path-scoped `git stash push --all`; verify ignored files outside selected paths remain untouched. |
| Q-04 | Use a normal Git stash entry with a clear message; capture the exact created ref and drop only after successful restoration. |
| Q-05 | Create one path-scoped stash before sync operational phases and restore it once at command exit according to the recovery policy. |
| Q-06 | Do not auto-restore after update creates conflict state; leave the stash intact and print instructions to resolve the Braid update before applying the stash manually. |
| Q-07 | Allow staged selected mirror-path changes; restore worktree state with `git stash apply <oid>` and selected mirror-path index state from `<oid>^2` instead of whole-stash `apply --index`. |

## Implementation Tasks

Tasks are tracked in `20-task-board.yaml`.

- `PLAN-QUESTIONS`: resolve open questions through grill-me.
- `PLAN-APPROVAL`: review and accept or revise the plan.
- `T01`: add CLI parser and usage.
- `T02`: add Git stash and ignored-aware status plumbing.
- `T03`: implement update status and sync autostash orchestration.
- `T04`: add command and integration coverage.
- `T05`: update docs and run full gate.

## Completion Criteria

- All open questions are resolved and recorded.
- User has reviewed and accepted the plan.
- All planned tasks in `20-task-board.yaml` are completed.
- Default sync behavior remains covered.
- README reflects final behavior.
- Targeted gates pass during implementation.
- `bazel test //...` passes before completion.
- Task evidence is recorded.
- Working tree is clean or any exception is explicitly documented.
