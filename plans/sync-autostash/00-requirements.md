# Braid Sync Autostash Requirements

Status: accepted
Date: 2026-06-21

## Mission

Allow `braid sync` to run when selected mirror paths contain uncommitted
changes, without weakening the existing safety boundary around `.braids.json`,
unresolved Git operations, or unrelated repository state.

The feature should protect the user's selected mirror-path work, run the normal
sync workflow, and restore the protected work with index state preserved when
Git can do so safely.

## Evidence From Existing Code And Tools

- `SyncHandler.Run` currently selects targets, calls `ensureSyncTargetsClean`,
  optionally pushes committed mirror changes, then delegates each target to
  `UpdateHandler.updateOne`.
- `ensureSyncTargetsClean` passes selected mirror paths and `.braids.json` to
  `ensureCommandScopesClean`, which blocks any `git status --porcelain` output
  under those scopes.
- Push planning compares committed `HEAD` mirror content with the recorded
  upstream revision. Uncommitted mirror changes are not part of the push phase.
- `UpdateHandler.updateOne` writes update commits through a temporary index and
  then restores only the mirror path and `.braids.json` from `HEAD`.
- Local Git is `git version 2.51.0`; `git stash push -h` confirms support for
  `--all`, `--include-untracked`, `--pathspec-from-file`,
  `--pathspec-file-nul`, and pathspec arguments.
- A local temp-repo probe confirmed that `git stash push --include-untracked --
  <paths>` followed by `git stash apply --index` restores staged and unstaged
  state for the same file and restores untracked path-scoped files.
- A local temp-repo probe confirmed that `git stash push --all -- mirror`
  captures ignored files under the selected path while leaving ignored files
  outside the selected path untouched.
- A local temp-repo probe confirmed that `git status --porcelain --ignored --
  mirror` reports ignored-only files under the selected path.
- A local temp-repo probe confirmed that `git stash drop <oid>` fails with
  `not a stash reference`; drop needs a current stash reflog selector, not just
  the saved stash commit OID.
- During T02 implementation, a real-repo test showed that path-scoped
  `git stash push --all` can still record unrelated staged index state in the
  stash index parent. `git stash apply --index <oid>` can then replay unrelated
  index state and fail when unrelated outside paths have both staged and
  unstaged changes.
- The verified restoration strategy is to run `git stash apply <oid>` without
  `--index`, then restore only selected mirror paths to the index from the
  stash index parent with `git restore --source=<oid>^2 --staged -- <paths>`.

## Scope

In scope:

- Add an opt-in sync flag for dirty selected mirror paths.
- Preserve current default behavior when the new flag is absent.
- Keep unresolved Git operation state as a hard blocker.
- Keep dirty `.braids.json` as a hard blocker.
- Protect only selected mirror paths, not unrelated paths.
- Include untracked and ignored files under selected mirror paths.
- Preserve index state on restoration when Git can apply it.
- Leave saved user work recoverable if restoration fails.
- Add unit, command, integration, and documentation coverage.

Out of scope:

- Do not push uncommitted mirror changes upstream.
- Do not autostash `.braids.json`.
- Do not add fallback compatibility behavior for older Git versions unless the
  supported minimum Git version cannot perform the selected approach.
- Do not change `braid push`, `braid update`, `braid status`, or `braid diff`
  public behavior except for internal refactoring required by sync autostash.
- Do not automatically resolve conflicts between restored user work and updated
  mirror content.

## Locked Decisions And Non-Negotiables

- Q-01 resolved: the public flag name is `--autostash`.
- Q-02 resolved: `--autostash` applies to both default `sync` and
  `sync --pull-only`.
- Q-03 resolved: ignored files are captured when they are under selected mirror
  paths, using path-scoped Git stash behavior.
- Q-04 resolved: Braid uses a normal Git stash entry with a clear message,
  captures the exact created ref, and drops it only after successful restore.
- Q-05 resolved: Braid creates one path-scoped stash before operational phases
  and restores it once at command exit according to the recovery policy.
- Q-06 resolved: if update creates conflict state, Braid does not auto-restore
  the stash; it leaves the stash intact and prints recovery instructions.
- Q-07 resolved and revised by implementation evidence: staged selected
  mirror-path changes are allowed, but automatic restoration must not use
  whole-stash `git stash apply --index`; it must restore selected mirror-path
  index state from the stash index parent after applying the worktree state.
- Default `braid sync` behavior remains unchanged when `--autostash` is absent.
- Dirty `.braids.json` remains a hard blocker.
- Unresolved Git operation state remains a hard blocker.
- Uncommitted mirror changes are not pushed upstream by this feature.

## Command Surface

Draft command:

```bash
braid sync [--autostash] [--pull-only] [--keep] [local_path...]
```

Behavior when the flag is absent:

- Unchanged from current `braid sync`: dirty selected mirror paths fail with
  `local changes are present in <path>`.

Behavior when the flag is present:

- If selected mirror paths are dirty, Braid records and removes those selected
  path changes before push/update work begins.
- Braid runs the existing sync push and update phases.
- Braid restores the saved selected path changes after the sync operation when
  doing so is safe.
- If restoration cannot complete, Braid leaves the saved state recoverable and
  prints explicit recovery instructions.

## Behavior Expectations

1. `--autostash` must be opt-in.
2. The existing dirty mirror-path failure remains the default.
3. Unresolved Git operation state must block before any autostash attempt.
4. Dirty `.braids.json` must block before any autostash attempt.
5. Mirror path overlap with `.braids.json` must still fail before side effects.
6. `--autostash` must only capture selected mirror paths.
7. No-path `braid sync --autostash` must only capture dirty paths for the
   selected no-path sync target set.
8. Explicit `braid sync --autostash vendor/a` must not capture dirty
   `vendor/b`.
9. Captured state must include tracked staged changes, tracked unstaged changes,
   tracked deletions, untracked files, and ignored files under selected mirror
   paths.
10. Captured state must not include untracked or ignored files outside selected
    mirror paths.
11. The push phase must continue to use committed `HEAD` content only.
12. The pull/update phase must continue to use existing update semantics.
13. On successful sync and successful restoration, the stash/saved entry must be
    removed.
14. On restoration failure, the stash/saved entry must remain recoverable.
15. On operational sync failure after saving user work, Braid must attempt to
    restore saved work unless doing so would obscure an update conflict state.
16. Recovery instructions must name the saved state and explain the next manual
    Git command.
17. The implementation must avoid mutating the user's unrelated index or
    worktree state.
18. `--autostash` must apply to both default `sync` and `sync --pull-only`.
19. Autostash state must be stored as a normal Git stash entry, not a
    Braid-owned private ref or custom snapshot.
20. Braid must capture the exact stash entry it created and must never apply or
    drop an unrelated user stash entry.
21. Braid must create at most one autostash entry for a sync invocation and
    restore it at command exit according to the resolved recovery policy.
22. Autostash dirty detection must be ignored-aware. A selected mirror path that
    only contains ignored files must still trigger autostash when `--autostash`
    is present.
23. Default sync without `--autostash` must keep the current non-ignored
    cleanliness behavior; ignored-only selected mirror files do not newly block
    default sync.
24. If an update writes conflict markers and `MERGE_MSG`, Braid must not
    auto-restore the autostash entry.
25. In update-conflict state, Braid must leave the autostash entry intact and
    print instructions to resolve the Braid update first, then apply the saved
    stash manually.
26. Update internals must expose conflict state to sync through a typed result
    or equivalent explicit status, because existing conflict handling can return
    success after writing conflict markers and `MERGE_MSG`.
27. Staged selected mirror-path changes must be allowed with `--autostash`.
28. When automatic restoration is allowed, Braid must apply the saved stash
    without `--index`, then restore only selected mirror paths to the index from
    the stash index parent.
29. Stash application may use the saved stash commit OID, but stash dropping
    must resolve and verify a current stash reflog selector for the saved entry
    before removing it.
30. If the saved stash entry cannot be found unambiguously for drop, Braid must
    leave it recoverable and print manual cleanup instructions rather than
    risking deletion of another stash entry.
31. If an autostash entry exists and update reaches conflict state, sync must
    stop, skip automatic stash restoration, leave the stash intact, and return a
    command error so the process exits non-zero.
32. Existing update conflict details and Braid conflict-resolution instructions
    must remain on stdout. Autostash recovery or cleanup failures must be
    reported through the command error path so they appear on stderr through the
    existing app error handling.
33. If an autostash entry exists and sync stops for update conflict, sync must
    not print repository-wide completion summaries such as skipped locked mirror
    output after the conflict because the sync did not complete.
34. If stash apply succeeds but stash cleanup/drop cannot safely complete, Braid
    must leave the restored work in place, leave the saved stash recoverable,
    and return a command error with manual cleanup instructions.

## Quality Gates

Required full gate:

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

## Known Intentional Divergences

- Git's `rebase --autostash` is operation-wide. Braid's draft behavior is
  mirror-path-scoped.
- A successful autostashed sync may end with user work reapplied as local dirty
  mirror changes. This is expected; the flag is about carrying local work over
  the update, not making the tree clean.
- Uncommitted mirror changes remain unpushed. Only committed mirror differences
  in downstream `HEAD` participate in the sync push phase.

## Open Questions Register

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | What should the public flag be named? | The user suggested `--auto-stash?`; Git uses `--autostash` on rebase/merge-like operations. | `--autostash`; `--auto-stash`; support both. | `--autostash` matches Git precedent and avoids aliases. `--auto-stash` is readable but less Git-native. Supporting both increases surface area. | Use only `--autostash`. | Accepted recommended default: use only `--autostash`. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
| Q-02 | resolved | Should `--autostash` apply to `--pull-only` sync as well as default sync? | Dirty selected mirror paths block because the update phase rewrites those paths; `--pull-only` still runs update. | Apply to both; apply only to default sync. | Both is consistent and solves the same blocker. Default-only leaves pull-only users blocked. | Apply to both default sync and `sync --pull-only`. | Accepted recommended default: apply to both default sync and `sync --pull-only`. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-03 | resolved | Should ignored files under selected mirror paths be captured? | Git supports `--all`, and a temp-repo probe confirmed `git stash push --all -- mirror` captures ignored files under the selected path while leaving ignored files outside that path untouched. Mirror sync can change mirror-local `.gitignore`, which may change whether local files are ignored after update. | Capture tracked plus untracked only; capture ignored too under selected mirror paths. | Excluding ignored files avoids moving generated artifacts but can lose or expose files whose ignored status changes due to mirror updates. Including ignored files is more comprehensive but can be slower and may move generated files under selected mirror paths during sync. | Do not capture ignored files. | Capture ignored files under selected mirror paths when Git path scoping can restrict them there. | `00-requirements.md`, `01-autostash-behavior-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-04 | resolved | Should Braid use the user's normal stash list or a Braid-owned ref/snapshot? | `git stash push` gives pathspec handling, ignored-file handling, and index preservation. A custom ref avoids visible stash entries but requires custom cleanup and replay logic. | Use normal stash entry; build Braid-owned snapshot. | Normal stash is simpler and familiar, but may leave an entry in the user's stash list after failures. Custom snapshot hides internals but is higher risk. | Use normal stash entry with a clear message and drop it only after successful restore. | Accepted recommended default: use a normal Git stash entry with a clear message, capture the exact created ref, and drop it only after successful restoration. | `00-requirements.md`, `01-autostash-behavior-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-05 | resolved | When should saved work be restored for multi-target sync? | Sync can push multiple mirrors and update multiple mirrors. Restoration can happen once or per target. | One save before all phases and one restore at the end; save/restore per target. | One save keeps phase behavior simple and avoids repeated stash stack churn. Per-target restoration narrows failure scope but complicates ordering and cross-target path overlap. | One save before operational phases and one restore after the command finishes or fails. | Accepted recommended default: create one path-scoped stash before operational phases and restore once at command exit according to the recovery policy. | `00-requirements.md`, `01-autostash-behavior-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
| Q-06 | resolved | What should happen if update writes conflict markers for a target? | Existing update writes conflict markers, stages `.braids.json`, writes `MERGE_MSG`, and instructs the user to resolve. Applying stashed work on top could obscure the conflict. | Do not auto-restore; leave stash and print recovery instructions. Try `stash apply --index` anyway. | Not restoring preserves Braid's conflict state clearly. Applying may be convenient when it works but can compound conflicts. | Do not auto-restore after an update conflict state is created. | Accepted recommended default: when update creates conflict state, leave the stash intact and print instructions to resolve the Braid update first, then apply the stash manually. | `00-requirements.md`, `01-autostash-behavior-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-07 | resolved | Should `--autostash` be allowed when selected mirror paths contain staged changes outside the mirror update scope? | The requested feature specifically mentions staging context preservation. Initial evidence suggested `apply --index`; T02 implementation evidence showed whole-stash `git stash apply --index` can replay unrelated outside index state and fail. | Allow and restore selected mirror-path index state; reject staged mirror-path changes. | Allowing meets the feature goal. Rejecting staged changes is safer but loses much of the value. Whole-stash `apply --index` is unsafe with unrelated staged+unstaged paths, so staged restoration must be selected-path scoped. | Allow staged selected mirror-path changes and restore selected mirror-path index state from the stash index parent. | Accepted recommended default, revised by implementation evidence: allow staged selected mirror-path changes; apply stash worktree state without `--index`, then restore only selected mirror paths to the index from `<stash-oid>^2`. | `00-requirements.md`, `01-autostash-behavior-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md`, `50-implementation-issues.md` |
