# Sync Autostash Behavior Deep Dive

Status: draft
Date: 2026-06-21

## Current Flow

Current `braid sync` flow:

1. Resolve repository and config.
2. Select sync targets.
3. Require selected mirror paths and `.braids.json` to be clean.
4. For default sync, hydrate missing recorded revisions if needed.
5. Build and run a push plan for committed mirror changes.
6. Run `updateOne` for each selected target.

The autostash feature should alter step 3 without weakening the rest of the
ordering. Dirty selected mirror paths become conditionally allowed only when the
new flag is present.

## Draft Autostash State Machine

1. Resolve targets.
2. Run immutable blockers:
   - unresolved Git operation state,
   - dirty `.braids.json`,
   - mirror path overlap with `.braids.json`.
3. Inspect selected mirror paths:
   - without `--autostash`, keep the existing non-ignored scoped cleanliness
     check,
   - with `--autostash`, use ignored-aware path status so ignored-only selected
     mirror state is saved.
4. If no selected mirror path is dirty, continue with normal sync.
5. If selected mirror paths are dirty and `--autostash` is absent, fail with the
   existing diagnostic.
6. If selected mirror paths are dirty and `--autostash` is present:
   - create a pathspec file containing selected mirror paths,
   - run one path-scoped stash for the whole sync invocation,
   - capture the created stash entry's commit OID and enough message/ref
     evidence to identify the stash-list entry later,
   - continue with normal sync using the cleaned selected paths.
7. At command exit:
   - if no saved state exists, return the sync result,
   - if sync produced an update conflict state, leave the saved state and print
     instructions to resolve the Braid update first, then apply the saved stash,
   - otherwise apply the saved state with index restoration,
   - drop the saved state only after apply succeeds.

## Draft Git Commands

The exact wrapper shape should stay behind `internal/gitexec`.

Path-scoped save:

```bash
git stash push --all -m "braid sync autostash" \
  --pathspec-from-file=<file> --pathspec-file-nul
```

Restore:

```bash
git stash apply <stash-oid>
git restore --source=<stash-oid>^2 --staged --pathspec-from-file=<file> --pathspec-file-nul
git stash drop <stash-ref>
```

Application must use the saved stash commit OID without `--index`, then restore
only selected mirror paths to the index from the stash index parent. This avoids
replaying unrelated index state that Git may record in a path-scoped stash.
Dropping must not use the OID directly, because `git stash drop <oid>` is not a
valid stash-list removal. The drop path must resolve the current `stash@{n}`
entry whose OID and Braid autostash message match the saved entry. If that
lookup is missing or ambiguous, Braid must leave the stash in place and print
manual cleanup instructions.
The saved state intentionally lives in the user's normal stash list so recovery
uses standard Git commands if Braid cannot restore it automatically.

## Update Result Contract

`UpdateHandler.updateOne` currently reports success for both ordinary successful
updates and conflict-marker fallback. Sync autostash needs an explicit result
contract so it can skip auto-restore after update conflict state.

Implementation should refactor update internals to return a result plus error,
such as:

- `updateStatusNoop`
- `updateStatusUpdated`
- `updateStatusConflict`

The result must report conflict state once conflict markers/config state have
been written, even if a later conflict-instruction or `MERGE_MSG` write returns
an error. This lets sync avoid applying the saved stash onto a partial conflict
state.

`UpdateHandler.Run` can preserve existing public success behavior by ignoring
the status where appropriate. `SyncHandler` must stop after
`updateStatusConflict` when an autostash entry exists, leave the autostash entry
intact, and return a command error with manual stash recovery instructions.

## Recovery Rules

Restoration succeeds:

- Drop the saved entry.
- Return the original sync result.

Sync fails before update conflict state:

- Attempt to restore saved work.
- If restoration succeeds, return the original sync error.
- If restoration fails, return an error that preserves both the sync failure and
  the saved-state recovery instructions.

Update writes conflict state:

- Do not attempt automatic restoration.
- Leave the saved entry.
- Keep existing update conflict details and Braid resolution commands on stdout.
- Return a command error so `sync --autostash` exits non-zero.
- Report that the stash was preserved and instruct the user to resolve the
  Braid update conflict first, then apply the saved stash manually.
- Do not print skipped locked mirror summaries or other completion summaries
  after the conflict because sync did not complete.

Partial update-conflict failure:

- If conflict markers or `.braids.json` conflict state were written and a later
  conflict-instruction or `MERGE_MSG` step fails, treat it as conflict state for
  autostash restoration decisions.
- Do not attempt automatic stash restoration.
- Return an error that includes both the underlying update error and the saved
  stash recovery instructions.

Restore fails:

- Leave the saved entry.
- Return a diagnostic that names the stash ref and suggests manual
  `git stash apply <stash-oid>` followed by selected-path index restoration from
  `<stash-oid>^2`.

Restore succeeds but drop/cleanup fails:

- Leave restored work in place.
- Leave the saved stash entry recoverable.
- Return a command error explaining that work was restored but stash cleanup did
  not complete.
- Include manual cleanup instructions using the verified selector when one is
  available; otherwise tell the user to inspect `git stash list`.

## Edge Cases To Cover

- Dirty mirror path without `--autostash` still fails.
- Dirty `.braids.json` with `--autostash` still fails.
- Unresolved Git operation with `--autostash` still fails.
- Explicit selected mirror dirty, non-selected mirror dirty.
- No-path sync with dirty selected branch/tag mirrors.
- No-path sync with dirty revision-locked mirror skipped by target selection.
- Staged and unstaged changes to the same file under a selected mirror path.
- Untracked file under a selected mirror path.
- Ignored file under a selected mirror path.
- Ignored-only selected mirror state triggers autostash.
- Ignored file outside selected mirror paths remains untouched.
- Deleted tracked file under a selected mirror path.
- Successful sync with autostash restores staged and unstaged context.
- `sync --pull-only --autostash` restores context.
- Push-plan failure after save restores context.
- Update conflict after save leaves stash and conflict state recoverable, with
  instructions to resolve the Braid update before applying the stash.
- Partial update-conflict failure after conflict markers or config state are
  written also skips auto-restore and reports both the update failure and stash
  recovery path.
- Restore conflict leaves stash recoverable.
- Successful restore with unresolved stash drop/cleanup returns a cleanup error
  and leaves the stash recoverable.
- Existing user stash entries before and after the Braid stash do not cause
  Braid to apply or drop the wrong entry.
- Unrelated staged and unstaged files outside selected mirror paths retain their
  exact index/worktree state across save, sync, restore, and restore failure.
- Staged selected mirror-path changes are restored by selected-path index
  restoration from `<stash-oid>^2`, not by whole-stash `apply --index`.
- `--keep` retains temporary Braid remotes and does not alter stash cleanup
  semantics.
