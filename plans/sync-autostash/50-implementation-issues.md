# Sync Autostash Implementation Issues

Status: active
Date: 2026-06-21

## I-01: Whole-Stash Index Apply Replays Unrelated Index State

- status: planned
- context: During T02, real-repo testing showed that a path-scoped
  `git stash push --all --pathspec-from-file=...` can still record unrelated
  staged index state in the stash index parent.
- evidence: `git stash apply --index <oid>` failed when an unrelated outside
  path had both staged and unstaged changes, attempting to replay that unrelated
  index state.
- why it matters: The accepted plan requires unrelated staged and unstaged state
  outside selected mirror paths to remain untouched.
- response: Keep staged selected mirror-path support, but do not use
  whole-stash `git stash apply --index`. Restore by applying the stash worktree
  state without `--index`, then restoring only selected mirror paths to the index
  from `<stash-oid>^2`.
- tracking tasks: T02, T03, T04.
- validation:
  - real Git plumbing test for selected staged/unstaged restoration,
  - real Git plumbing test for unrelated staged+unstaged preservation,
  - command test proving sync restores selected index state without mutating
    unrelated index state.

## I-02: Partial Conflict Failure Coverage Was Missing

- status: fixed
- context: Iterative implementation review round 1 found that the plan promised
  command coverage for failures after update conflict state is written.
- evidence: Existing sync autostash tests covered the normal conflict path, but
  did not force a later conflict-instruction or `MERGE_MSG` write failure.
- why it matters: Sync must leave the autostash intact whenever update reaches
  conflict state, including when reporting the conflict hits a later I/O error.
- response: Added a real-repository sync test that creates `.git/MERGE_MSG` as a
  directory before triggering an update conflict. The test proves stdout keeps
  conflict details, stderr reports the update failure and autostash recovery
  instructions, the dirty file remains stashed, and the conflict markers remain
  in the mirror path.
- tracking tasks: T03, T04.
- validation:
  - `bazel test //internal/command:command_test --test_output=streamed --test_filter=TestSyncCommandAutostashUpdateConflictWriteFailureLeavesStash`
  - `bazel test //internal/command:command_test --test_output=streamed --test_arg=-test.v`

## I-03: Post-Restore Cleanup Failure Coverage Was Missing

- status: fixed
- context: Iterative implementation review round 1 found that the plan promised
  command coverage for successful stash apply followed by unresolved stash drop
  or cleanup failure.
- evidence: Gitexec tests covered stash drop lookup failures, but command-layer
  coverage did not assert the user-facing cleanup error after restore succeeded.
- why it matters: Users need clear diagnostics that their work was restored and
  the saved stash remains recoverable if cleanup cannot remove it.
- response: Narrowed the restore helper dependency to the three Git methods it
  actually calls and added a command-package test with a fake restore backend
  that succeeds through apply and selected-path index restore, then fails drop.
- tracking tasks: T03, T04.
- validation:
  - `bazel test //internal/command:command_test --test_output=streamed --test_filter=TestSyncCommandAutostashRestoreReportsCleanupFailureAfterApply`
  - `bazel test //internal/command:command_test --test_output=streamed --test_arg=-test.v`
