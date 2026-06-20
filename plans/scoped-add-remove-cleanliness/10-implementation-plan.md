# Scoped Add/Remove Cleanliness Implementation Plan

Status: accepted
Date: 2026-06-20

## Phase Sequence

1. Finalize plan review.
2. Add Git plumbing helpers for in-memory config blobs and tree-minus-path
   composition.
3. Generalize scoped cleanliness helpers without relaxing add/remove preflight
   yet.
4. Rework `braid add` to use scoped checks, synthetic commit trees,
   temporary-index commits, narrow restore/cleanup, and add-specific preflight
   relaxation.
5. Rework `braid remove` to use scoped checks, synthetic commit trees,
   temporary-index commits, narrow restore/cleanup, and final global clean
   preflight removal.
6. Add integration coverage and README updates.
7. Run targeted gates and `bazel test //...`, then commit with the `commit` skill.

## Delivery Approach

- Pause after this draft plan is emitted and request user review.
- Do not edit implementation files until the plan review is complete.
- Execute one task at a time and keep `20-task-board.yaml` current.
- Prefer small behavior slices with their tests in the same task.
- Use `apply_patch` for manual edits.
- Keep product Git invocations inside `internal/gitexec`.
- Preserve unrelated user changes in the worktree and never use broad destructive
  rollback commands.
- Use the `commit` skill before every commit.

## Implementation Details

### Git Plumbing

- Add a `gitexec` helper that stores arbitrary bytes as a blob using
  `git hash-object -w --stdin`.
- Add a `gitexec` helper that returns a tree produced from a base tree with one
  path removed, using a temporary index.
- Add unit tests proving helpers do not alter the real index or worktree.

### Shared Scoped Cleanliness

- Add shared scoped helper code while add/remove still retain their current
  global clean preflight.
- Generalize update's scoped checks so callers can:
  - block unresolved Git operation states;
  - check `.braids.json` when required or when present;
  - check one or more command-owned paths with pathspec porcelain;
  - reject mirror paths overlapping `.braids.json`.
- Keep update behavior unchanged while moving any reusable helper code.
- Relax global clean preflight only inside the command-specific implementation
  tasks after the relevant command has scoped checks and temporary-index commit
  flow. Remove `Clean` from `Requirements` and remove the base preflight
  `StatusPorcelain` dependency only after both add and remove no longer need it.

### `braid add`

- Load config and derive the final mirror path before side effects.
- Validate the candidate path and reject `.braids.json` overlap.
- Run scoped checks before cache fetch, remote setup, or fetch.
- Reject an add target whose path shape is unavailable: tracked or indexed entry
  at the target, under the target, or at a blocking ancestor; worktree
  file/symlink at the target or a blocking ancestor; or untracked content under
  the target.
- Fetch/resolve upstream item as today.
- Marshal updated config in memory and hash it as a blob.
- Compose final tree from current `HEAD`, upstream item at the target path, and
  the config blob.
- Commit with `CommitTreeWithTemporaryIndex`.
- Restore only target path and `.braids.json` from `HEAD`.
- Remove the temporary remote after commit and narrow restore. If cleanup fails,
  return a post-commit cleanup error without rolling back `HEAD`.
- Remove `resetOnError` broad hard reset; use narrow cleanup only for any owned
  paths touched by the command.

### `braid remove`

- Load config and resolve the mirror before side effects.
- Run scoped checks on `.braids.json` and the mirror path before deletes, config
  writes, or remote removal.
- Treat missing tracked mirror content as dirty through the scoped path check.
- Marshal updated config in memory and hash it as a blob.
- Compose final tree from current `HEAD` with the mirror path removed and config
  blob added.
- Commit with `CommitTreeWithTemporaryIndex`.
- Restore only mirror path and `.braids.json` from `HEAD`.
- For non-`--keep` removes, remove the Braid-managed remote after commit and
  narrow restore. If cleanup fails, return a post-commit cleanup error without
  rolling back `HEAD`.
- Remove `resetRemoveOnError` broad hard reset; use narrow cleanup only if owned
  paths were touched before commit.

### README

- Replace the current add/remove global-clean wording with one shared rule for
  automatic-commit commands.
- Keep command-specific scope details short:
  - add: `.braids.json` if present plus absent/clean target path;
  - update: `.braids.json` plus target mirror paths;
  - remove: `.braids.json` plus removed mirror path.
- Clean up repeated wording where the shared rule makes it redundant.

## High-Risk Areas

- Real index contamination:
  - Impact: unrelated staged files could be included in Braid commits.
  - Mitigation: temporary-index commits plus tests checking `diff-tree` and
    staged blob preservation.
- Unsafe intermediate relaxation:
  - Impact: dropping global clean preflight before a command is reworked would
    allow unrelated dirty work while the old real-index commit and hard-reset
    rollback paths still exist.
  - Mitigation: T02 is helper-only; add/remove preflight relaxation happens only
    in T03/T04 with the matching command implementation.
- User data loss under target paths:
  - Impact: add/remove could overwrite or delete untracked files.
  - Mitigation: scoped porcelain checks include untracked blockers for command
    paths.
- Config races:
  - Impact: dirty `.braids.json` edits could be overwritten.
  - Mitigation: always block dirty config when add/remove will rewrite it.
- Post-commit restore failures:
  - Impact: working tree may not match new `HEAD` for owned paths.
  - Mitigation: no broad rollback; return explicit error and leave unrelated
    state untouched.
- Helper refactor regression in update:
  - Impact: update's accepted behavior could change accidentally.
  - Mitigation: run existing update unit and integration tests as part of command
    and full gates.

## Required Gates

Targeted gates during implementation:

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

Full gate before claiming completion:

```bash
bazel test //...
```

## Completion Criteria

- No open questions remain in `00-requirements.md`.
- Plan review has been requested and accepted.
- Add/remove preserve unrelated staged, unstaged, and untracked work.
- Add/remove automatic commits exclude unrelated staged entries.
- Dirty command-owned scopes block with path-specific diagnostics.
- No add/remove broad `reset --hard` rollback remains.
- README describes the final behavior succinctly.
- Targeted gates and `bazel test //...` pass.
- Task board evidence and commit metadata are recorded.

## Decision Log

| Question | Plan outcome |
| --- | --- |
| Q-01 | Add uses scoped cleanliness: existing config clean if present, target clean/absent, unresolved ops block, unrelated work preserved. |
| Q-02 | Remove uses scoped cleanliness: config and mirror path clean, unrelated work preserved. |
| Q-03 | Add/remove automatic commits use temporary indexes. |
| Q-04 | Successful add/remove restore only command-owned paths from `HEAD`. |
| Q-05 | Add/remove broad hard rollback is removed. |
| Q-06 | Failed add cleans up temporary remote and only add-owned paths. |
| Q-07 | Failed remove restores only mirror path and config. |
| Q-08 | Remove `--keep` and remote cleanup behavior remain unchanged. |
| Q-09 | Add temporary remote cleanup behavior remains unchanged. |
| Q-10 | Dirty `.braids.json` blocks add/remove. |
| Q-11 | Add target path must have no existing tracked, staged, unstaged, or untracked content. |
| Q-12 | Untracked files under removed mirror path block remove. |
| Q-13 | Add rejects mirror paths overlapping `.braids.json`. |
| Q-14 | Add/remove scoped checks move from preflight to handlers. |
| Q-15 | The global `Clean` preflight surface is removed only after add/remove replacement scoped checks are implemented. |
| Q-16 | Scoped helper is generalized for update/add/remove. |
| Q-17 | First add still works without an existing `.braids.json`. |
| Q-18 | Scoped checks run before remote/cache/file side effects. |
| Q-19 | Add composes a synthetic final tree and commits it through a temporary index. |
| Q-20 | Remove composes a synthetic final tree with mirror path removed and commits it through a temporary index. |
| Q-21 | Pre-commit cleanup preserves original error and reports cleanup failure if cleanup fails. |
| Q-22 | Post-commit restore failure is reported without rolling back `HEAD`. |
| Q-23 | Add avoids real worktree writes until after the temp-index commit where feasible. |
| Q-24 | Updated config is hashed from in-memory bytes using Git. |
| Q-25 | Add rejects clean tracked target content in `HEAD`. |
| Q-26 | Missing tracked remove mirror path blocks as local deletion. |
| Q-27 | Add/remove explicitly block unresolved Git operation states. |
| Q-28 | Unit tests cover preservation, blockers, unresolved state, and plumbing. |
| Q-29 | Integration tests cover add preservation, remove preservation, and representative blockers. |
| Q-30 | README uses one shared scoped-cleanliness rule with command-specific scopes and trims redundant wording. |
| Q-31 | Existing update plan remains historical; this scope uses a new plan tree. |

## Review Loop Adjustments

| Round | Finding | Assessment | Plan change |
| --- | --- | --- | --- |
| 1 | Unsafe sequencing for global clean removal | Valid | Made T02 helper-only; command preflight relaxation moves into T03/T04 after each command's replacement flow exists. |
| 1 | Remove remote lifecycle ambiguity | Valid | Defined commit and narrow restore before remote cleanup, with explicit post-commit cleanup error and no rollback. |
| 1 | Failure-path validation gaps | Valid | Added fake-Git failure-path tests for cleanup failures, remote cleanup failures, and post-commit restore failures. |
| 1 | Add target path-shape conflicts underspecified | Valid | Defined add target availability across target, descendants, and blocking ancestors; added corresponding test requirements. |
