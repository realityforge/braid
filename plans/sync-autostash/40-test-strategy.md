# Braid Sync Autostash Test Strategy

Status: draft
Date: 2026-06-21

## Required Gates

Full gate:

```bash
bazel test //...
```

Targeted gates:

```bash
bazel test //internal/cli:cli_test
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

## Unit And Command Coverage

CLI:

- Parse the selected flag for `sync`.
- Preserve existing `sync` path and flag parsing.
- Reject unknown flags with existing diagnostics.
- Include the flag in command usage.

Git execution:

- Path-scoped stash captures tracked staged changes.
- Path-scoped stash captures tracked unstaged changes.
- Path-scoped stash captures staged and unstaged changes to the same file.
- Path-scoped stash captures tracked deletions.
- Path-scoped stash captures untracked files.
- Path-scoped stash captures ignored files under selected paths when using
  `--all`.
- Path-scoped stash does not capture non-selected paths.
- Path-scoped stash does not capture ignored files outside selected paths.
- Ignored-aware selected-path status reports ignored-only selected mirror state.
- Stash worktree apply without `--index` plus selected-path restore from the
  stash index parent restores selected mirror-path index state.
- Drop only removes the intended saved entry.
- Drop resolves the current stash reflog selector for the saved entry rather
  than dropping by OID.
- Missing or ambiguous stash lookup leaves the saved entry recoverable.
- Existing user stash entries do not confuse save/apply/drop targeting.
- Unrelated staged and unstaged paths outside selected pathspecs retain their
  exact state across save/apply/drop and apply failure.
- Whole-stash `git stash apply --index` is not used for automatic restoration.

Command behavior:

- Dirty selected mirror without the flag fails with the current diagnostic.
- Dirty selected mirror with the flag proceeds.
- Staged selected mirror-path changes with the flag are restored to the index
  from the stash index parent when automatic restoration is allowed.
- Ignored-only selected mirror state with the flag is saved and restored.
- Ignored-only selected mirror state without the flag does not newly block
  default sync.
- Dirty `.braids.json` with the flag fails.
- Unresolved Git operation with the flag fails.
- Explicit selected mirror dirty plus non-selected mirror dirty only saves the
  selected mirror path.
- Unrelated staged and unstaged paths outside selected mirror paths are
  preserved across successful sync and failed restoration.
- No-path sync skips revision-locked mirrors before autostash handling.
- Default sync with committed local changes still pushes only committed `HEAD`
  content.
- `sync --pull-only` follows the resolved autostash policy.
- Operational failure after saving user work attempts restoration according to
  the resolved policy.
- Update conflict after saving user work leaves the stash intact and prints
  instructions to resolve the Braid update before applying the stash manually.
- Update conflict is detected through explicit update status even though the
  existing public update behavior treats conflict-marker fallback as success.
- `sync --autostash` with a saved stash and update conflict returns non-zero,
  keeps update conflict details on stdout, and reports autostash recovery through
  the command error path.
- `sync --autostash` does not print skipped locked mirror summaries after an
  update conflict that leaves the saved stash intact.
- Partial update-conflict failure after conflict markers or `.braids.json`
  conflict state are written skips stash restoration and reports both the update
  failure and stash recovery path.
- Restoration failure leaves saved work recoverable.
- Successful stash apply followed by unresolved stash drop/cleanup leaves
  restored work in place, leaves the stash recoverable, and returns a cleanup
  error with manual cleanup instructions.
- Existing `braid update` conflict behavior remains success with stdout
  resolution instructions.

## Integration Coverage

- Executable `braid sync --autostash <path>` with staged and unstaged selected
  mirror work:
  - sync updates the mirror,
  - saved work is restored,
  - selected mirror-path index state is preserved,
  - unrelated outside index/worktree state is preserved.
- Executable `braid sync --pull-only --autostash <path>` with dirty selected
  mirror work restored after update.
- Executable conflict recovery path proves update conflict skips auto-restore,
  leaves the stash intact, and prints manual recovery instructions.

## Regression Coverage

- Existing sync push-then-update behavior remains covered.
- Existing dirty selected mirror failure remains covered.
- Existing scoped dirty non-selected mirror behavior remains covered.
- Existing update conflict behavior remains covered.
