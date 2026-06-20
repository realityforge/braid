# Scoped Add/Remove Cleanliness Requirements

Status: accepted
Date: 2026-06-20
Base: `75c3ab2 feat(update): preserve unrelated work during updates`

## Mission

Implement the same unrelated-work preservation behavior for `braid add` and
`braid remove` that `braid update` now provides: command-owned paths must be
clean, while unrelated staged, unstaged, and untracked work is preserved exactly
and excluded from Braid's automatic commits.

## Scope

In scope:

- Relax `braid add` and `braid remove` from whole-worktree cleanliness to
  command-scoped cleanliness.
- Preserve unrelated index and worktree state for successful and failed
  add/remove operations.
- Remove broad `reset --hard` rollback from add/remove paths.
- Remove the now-unneeded global `Clean` preflight surface only after add/remove
  handlers have replacement scoped checks and temporary-index commit paths.
- Add focused unit tests and binary-level integration tests.
- Update README wording so automatic-commit commands share one concise scoped
  cleanliness rule with command-specific scopes.
- Add this plan tree under `plans/scoped-add-remove-cleanliness/` without
  modifying the historical update plan.

Out of scope:

- Changing `braid update` behavior beyond any shared helper movement needed by
  this work.
- Changing `status`, `diff`, `push`, or `setup` behavior.
- Adding fallback Git engines or compatibility shims.
- Preserving or merging dirty `.braids.json` edits.
- Committing any implementation before plan review is complete.

## Starting Evidence

- At plan start, `braid add` had `Clean: true` preflight and used normal
  real-index commit flow in `internal/command/add.go`.
- At plan start, `braid remove` had `Clean: true` preflight and used normal
  real-index commit flow in `internal/command/remove.go`.
- Both add and remove used broad `reset --hard` rollback.
- `braid update` already has scoped cleanliness, unresolved-operation checks,
  temporary-index commits, and narrow restore helpers.
- At plan start, the repository had no add/remove preservation integration
  coverage.

## Implementation Outcome

- `braid add` and `braid remove` now use handler-level scoped cleanliness checks
  and temporary-index automatic commits.
- The global `Clean` preflight field and base `StatusPorcelain` preflight
  dependency have been removed.
- Add/remove unit tests and executable-level integration tests cover unrelated
  staged, unstaged tracked, and untracked work preservation plus representative
  scoped blockers.

## Locked Decisions

- Add/remove must block unresolved Git operation state before writing or
  committing.
- Add/remove must preserve unrelated staged, unstaged tracked, and untracked work
  exactly.
- Add/remove automatic commits must use a temporary index so unrelated staged
  entries are excluded.
- No broad `reset --hard` rollback is allowed once unrelated work is permitted.
- Product Git operations must remain behind `internal/gitexec`.
- Manual edits must use `apply_patch`.
- Targeted Bazel tests and `bazel test //...` are required before claiming
  completion.
- The `commit` skill is mandatory before any commit.

## Command Behavior

### `braid add`

- Derive and validate the final mirror path before remote/cache work.
- If `.braids.json` exists, require it to be clean in index and working tree.
- Allow first add when `.braids.json` is absent.
- Reject mirror paths that are `.braids.json` or under `.braids.json/`.
- Require the target path to be absent from `HEAD`.
- Require the target path to have no tracked, staged, unstaged, or untracked
  content before add.
- Require target availability across the full path shape: no tracked or indexed
  entry at the target, under the target, or at a blocking ancestor; no worktree
  file/symlink at the target or a blocking ancestor; and no untracked content
  under the target.
- Treat untracked files under the target path as blockers.
- Compose the add commit from current `HEAD`, the upstream item at the mirror
  path, and the updated `.braids.json`.
- Do not materialize command-owned files in the real worktree until after the
  temporary-index commit when feasible.
- After commit, restore only the target path and `.braids.json` from `HEAD`.
- Preserve existing temporary remote cleanup semantics on success and failure.
  On success, run temporary remote cleanup after the commit and narrow restore.
  If that cleanup fails, report an explicit post-commit cleanup error without
  rolling back `HEAD`.

### `braid remove`

- Require `.braids.json` and the mirror path to be clean in index and working
  tree.
- Treat a missing tracked mirror path as dirty local deletion and block removal.
- Treat untracked files under the mirror path as blockers.
- Compose the remove commit from current `HEAD` with the mirror path removed and
  updated `.braids.json` added.
- After commit, restore only the mirror path and `.braids.json` from `HEAD`.
- Preserve `--keep` behavior and ordinary Braid-managed remote removal behavior.
  For non-`--keep` removes, delete the Braid-managed remote after the commit and
  narrow restore. If remote cleanup fails, report an explicit post-commit cleanup
  error without rolling back `HEAD`.

## Failure Behavior

- Failed add/remove operations before a commit should clean up only
  command-owned paths where those paths were touched.
- Add failure cleanup should remove the temporary remote if add created one.
- Cleanup success should return the original command error.
- Cleanup failure should return an error that includes the original cause and the
  narrow cleanup failure.
- If the temporary-index commit succeeds but post-commit restore fails, leave
  `HEAD` at the new Braid commit and return an explicit restore error.
- If post-commit remote cleanup fails for add or remove, leave `HEAD` at the new
  Braid commit and return an explicit cleanup error.
- Do not roll back `HEAD` broadly after it moves.

## Git Plumbing Requirements

- Reuse existing update helpers where they fit.
- Add `internal/gitexec` support as needed for:
  - hashing in-memory config bytes with `git hash-object -w --stdin`;
  - composing a tree from a base tree with one path removed.
- Keep direct Git invocation outside product command code.
- Prefer tree/index plumbing over writing to the real worktree before commit.

## Test And Quality Gates

Targeted gates:

- `bazel test //internal/gitexec:gitexec_test`
- `bazel test //internal/command:command_test`
- `bazel test //integration:braid_integration_test`

Full gate:

- `bazel test //...`

Coverage requirements:

- Unit tests for add preservation, add scoped blockers, remove preservation,
  remove scoped blockers, unresolved-operation blocking, Git plumbing helpers,
  and preflight cleanup.
- Integration tests for add preservation, remove preservation, and representative
  scoped blocker behavior.
- README checks by review and test-backed behavior examples.

## Open Questions Register

All questions are resolved from the planning interview. The plan remains draft
until user review is requested and the review outcome is recorded.

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | What is add scoped cleanliness? | Add writes a new target and config. | Whole repo clean; command-owned paths clean. | Scoped improves usability but needs stricter target checks. | Existing config clean if present, target clean/absent, unresolved ops block, preserve unrelated work. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-02 | resolved | What is remove scoped cleanliness? | Remove writes config and deletes a mirror. | Whole repo clean; config plus mirror clean. | Scoped preserves unrelated work while keeping owned paths safe. | `.braids.json` plus removed mirror path clean; preserve unrelated work. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-03 | resolved | Should add/remove commits use temporary indexes? | Normal commits include unrelated staged files. | Normal commit; temporary-index commit. | Temp index adds plumbing but preserves real index. | Use temporary-index commits. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-04 | resolved | How should successful add/remove materialize results? | Temp-index commits do not update the real index/worktree. | Leave state; restore owned paths. | Narrow restore gives normal postcondition without touching unrelated paths. | Restore command-owned paths from `HEAD` after commit. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-05 | resolved | Should add/remove remove broad hard rollback? | `reset --hard` would destroy unrelated work. | Keep reset; narrow cleanup/no rollback. | Narrow cleanup is safer but needs explicit error paths. | Remove broad `reset --hard`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-06 | resolved | How should failed add clean up? | Add may create a remote and owned paths. | Hard reset; remote cleanup plus narrow owned-path cleanup. | Narrow cleanup preserves unrelated work. | Restore/remove only add-owned paths and cleanup temporary remote. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-07 | resolved | How should failed remove clean up? | Remove may delete owned paths before commit. | Hard reset; restore owned paths. | Narrow restore preserves unrelated work. | Restore only mirror path and `.braids.json` from `HEAD`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-08 | resolved | Should remove remote behavior change? | Scope is cleanliness and preservation. | Preserve; refactor lifecycle. | Preserving avoids unrelated behavior change. | Keep `--keep` and ordinary remote removal behavior unchanged. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-09 | resolved | Should add remote cleanup behavior change? | Add already removes temporary remotes. | Preserve; refactor lifecycle. | Preserving avoids unrelated behavior change. | Keep success/failure temporary remote cleanup semantics. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-10 | resolved | Should dirty `.braids.json` block add/remove? | Config is rewritten as one structured file. | Allow; block. | Blocking avoids dropped user edits. | Block dirty config. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-11 | resolved | Should add target path need no existing content? | Add creates a new mirror. | Allow clean existing path; require absent/clean. | Requiring absent prevents absorbing product files. | Target must have no tracked/staged/unstaged/untracked content. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-12 | resolved | Should untracked files under removed mirror block remove? | Untracked files are user data. | Allow deletion; block. | Blocking avoids data loss. | Block removal. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-13 | resolved | Should add reject paths overlapping `.braids.json`? | Add writes both target and config. | Allow; reject. | Rejecting matches update overlap safety. | Reject `.braids.json` and descendants. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-14 | resolved | Should add/remove scoped checks move out of global preflight? | Preflight cannot know final add target cleanly. | Keep global clean; handler scoped checks. | Handler checks support command-specific paths. | Move checks into handlers and drop `Clean: true`. | Accepted recommendation; also investigate removing `Clean` entirely. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-15 | resolved | Is the `Clean` preflight surface still needed? | Only add/remove currently use it. | Keep unused surface; remove. | Removing simplifies preflight. | Remove `Clean` and base `StatusPorcelain` preflight dependency if unused. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-16 | resolved | Should scoped helper be shared? | Update already has scoped helpers. | Duplicate; generalize. | Shared helper avoids divergence. | Generalize helper for update/add/remove. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-17 | resolved | How should missing `.braids.json` affect add? | First add currently works without config. | Require config; allow absent for add. | Allowing absent preserves existing first-add UX. | Check config only when it exists for add; always for remove/update. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-18 | resolved | When should scoped checks run? | Remote/cache work can create side effects. | After setup/fetch; before side effects. | Early checks reduce partial work. | Run after final path known and before remote/cache/file work. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-19 | resolved | How should add compose the commit? | Add currently writes worktree/index first. | Real index; synthetic tree plus temp commit. | Synthetic tree avoids real-index pollution. | Compose from `HEAD` plus upstream item plus config blob, then temp-index commit. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-20 | resolved | How should remove compose the commit? | Remove currently uses `git rm` and normal commit. | Real index; synthetic tree minus path plus config blob. | Synthetic tree excludes unrelated staged files. | Add helper for base tree with path removed, then temp-index commit. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-21 | resolved | How should pre-commit add cleanup report errors? | Cleanup can fail. | Hide cleanup failure; combine errors. | Combining preserves root cause and cleanup evidence. | Return original error if cleanup succeeds, otherwise combined error. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-22 | resolved | What if post-commit restore fails? | `HEAD` already moved. | Roll back; report and leave `HEAD`. | No rollback protects unrelated work. | Return explicit restore error and leave `HEAD` at new commit. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-23 | resolved | Should add avoid touching worktree before commit? | Worktree writes create cleanup burden. | Materialize early; commit then restore. | Commit then restore is cleaner. | Avoid real worktree writes until after temp-index commit where feasible. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-24 | resolved | How should updated config be hashed? | Writing config early dirties worktree. | Write file then hash; hash bytes through Git. | Hashing bytes avoids early worktree mutation. | Use in-memory `git hash-object -w --stdin`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-25 | resolved | Should add reject clean tracked target content? | Porcelain would not report clean tracked files. | Allow; reject tracked target in `HEAD`. | Rejecting prevents replacing product code. | Reject if target exists in `HEAD`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-26 | resolved | Should missing remove mirror path block? | Missing tracked content is local deletion. | Proceed; block. | Blocking keeps remove from absorbing local deletion. | Block as dirty mirror scope. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-27 | resolved | Should unresolved Git operations block add/remove? | Global clean used to block many unresolved states. | Allow; block explicitly. | Blocking avoids automatic commits during in-progress operations. | Reuse `BlockingOperation`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-28 | resolved | What unit tests are mandatory? | Preservation and blockers are behavior-critical. | Minimal; broad focused unit coverage. | More unit coverage catches command edge cases cheaply. | Add the accepted add/remove preservation, blocker, and unresolved-operation unit tests. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-29 | resolved | What integration tests are mandatory? | Real Git/index behavior matters. | Smoke only; preservation plus representative blockers. | Representative integration keeps runtime contained. | Add add preservation, remove preservation, and scoped blocker integration tests. | Accepted recommendation; integration should be fairly broad. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-30 | resolved | How should README describe the behavior? | Current README says add/remove require clean worktree. | Separate wording; shared rule with scopes. | Shared rule is more succinct and avoids repetition. | One shared scoped-cleanliness rule with add/update/remove scopes. | Accepted recommendation; also clean up related README wording where possible. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-31 | resolved | Should the old update plan be edited? | It records the previous update-only scope. | Rewrite history; create new plan tree. | New tree preserves historical accuracy. | Leave old update plan intact and create this add/remove plan tree. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
