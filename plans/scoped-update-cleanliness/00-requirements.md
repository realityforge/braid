# Scoped Update Cleanliness Requirements

Status: accepted
Date: 2026-06-20

## Mission

Allow `braid update` to run when unrelated local work exists, as long as the update target scope is clean:

- `.braids.json`
- the mirror path or paths that `braid update` will touch

The update must preserve the user's unrelated index and working tree state and must not include unrelated changes in automatic Braid update commits.

## Evidence From Current Code

- `Preflight` currently enforces a global clean worktree for update through `git status --porcelain`.
- `RequirementsFor(cli.CommandUpdate)` currently includes `Clean: true`.
- `UpdateHandler.updateAll` currently re-checks global cleanliness between mirror updates.
- `UpdateHandler.updateOne` writes `.braids.json`, stages it with `git add`, and commits through normal Git index state.
- `gitexec.MakeTreeWithItemIn` already uses temporary indexes for synthetic trees.
- The current global clean check also blocks unresolved Git operation states indirectly because unmerged index entries make `git status --porcelain` non-empty.
- Local probing showed:
  - `git commit-tree` skips `post-commit`, unlike the current `git commit --no-verify` path.
  - `git commit --no-verify` can commit from a temporary `GIT_INDEX_FILE`.
  - `git merge-recursive` refuses to run with unrelated staged entries.
  - `git merge-tree --write-tree` performs tree-level merges without touching the real index or worktree.

## Scope Boundaries

In scope:

- Relax cleanliness only for `braid update`.
- Preserve unrelated staged changes, unstaged tracked edits, and untracked files outside the update scope.
- Keep `.braids.json` and target mirror path cleanliness strict.
- Block updates when the repository is already in an unresolved Git operation state.
- Support single-path update and update-all.
- Support directory mirrors and single-file mirrors.
- Preserve current one-commit-per-updated-mirror behavior.
- Preserve `--keep` remote behavior.
- Preserve automatic commit `--no-verify` behavior.
- Update README behavior text.
- Add unit and integration tests.

Out of scope:

- Relaxing `braid add` or `braid remove`.
- Adding backwards-compatibility fallback merge engines.
- Supporting mirror paths that overlap `.braids.json`.
- Broad automatic rollback after `HEAD` has moved.
- Changing update history shape or batching multiple mirrors into one commit.

## Locked Decisions And Non-Negotiables

- `braid update` must not require the whole worktree to be clean.
- Scoped cleanliness means clean in both index and working tree relative to `HEAD`.
- Untracked files under the target mirror path block update.
- Ignored files remain allowed.
- `.braids.json` changes always block update.
- Update-all checks all eligible target paths before the first fetch, remote setup, cache mutation, worktree write, or commit.
- Update-all skips revision-locked mirrors before scoped cleanliness checks.
- Unrelated dirty paths are allowed only for ordinary staged, unstaged, and untracked file state; existing unmerged entries and in-progress Git operation sentinels such as `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`, `REBASE_HEAD`, `rebase-merge`, and `rebase-apply` block update.
- A dirty mirror not targeted by `braid update <path>` does not block that command.
- Automatic update commits must contain only the mirror path and `.braids.json`.
- Unrelated user index state must survive byte-for-byte from Git's perspective.
- Conflict handling must preserve unrelated index state.
- Conflict handling must stage `.braids.json` at the new revision.
- Conflict handling must write `MERGE_MSG`.
- Conflict handling should create proper unmerged index stages if practical.
- If proper unmerged index stages are not implemented, the accepted fallback is exact and testable: mirror files with conflicts are written with conflict markers in the working tree and left unstaged, `.braids.json` is staged at the new revision, no unmerged index entries are created by Braid, `MERGE_MSG` is written, and output/docs explain that the user must resolve markers, stage the mirror path and `.braids.json`, then commit with `git commit -F .git/MERGE_MSG` or an equivalent command that uses the generated merge message.
- The implementation may rely on Git `merge-tree --write-tree` under the existing minimum Git version.

## Command Surface And Behavior Expectations

`braid update <local_path>`:

- Before path-scoped checks, reject existing unresolved Git operation state: unmerged index entries or in-progress operation sentinel files.
- Check `.braids.json` and `<local_path>` for staged, unstaged, deleted, or untracked changes.
- Allow unrelated dirty paths.
- Fetch and resolve the update as today.
- If already up to date, leave `HEAD`, index, and working tree untouched except for unchanged remote cleanup behavior.
- If updated without conflict:
  - Seed a temporary index from current `HEAD`.
  - Apply the merged mirror tree and updated `.braids.json` into that temporary index, including deletes and renames.
  - Run `git commit --no-verify` with `GIT_INDEX_FILE` pointing at the temporary index.
  - Restore only `<local_path>` and `.braids.json` in the real index/worktree from the new `HEAD`.
- If conflict occurs, materialize conflict markers only in `<local_path>`, update and stage `.braids.json`, write `.git/MERGE_MSG`, and preserve unrelated index/worktree state.

`braid update`:

- Load config and build the sorted target list of branch/tag mirrors.
- Skip revision-locked mirrors before cleanliness checks.
- Check for unresolved Git operation state, `.braids.json` changes, and every eligible target mirror path before any fetch, remote setup, cache mutation, worktree write, or commit.
- Stop before touching any mirror if a scoped path is dirty or the repository is already in an unresolved Git operation state.
- Keep the existing one-commit-per-mirror behavior for mirrors that actually update.

`braid add` and `braid remove`:

- Keep existing whole-worktree clean requirement.

## Diagnostics

- Scoped cleanliness failures must name the first offending path.
- Preferred message shape: `local changes are present in <path>`.
- Existing Git operation state failures must name the blocking state, for example unmerged index entries or `MERGE_HEAD`.
- Update-all should fail before any fetch, remote setup, cache mutation, worktree write, or commit if any eligible mirror path is dirty.
- Mirror path overlap with `.braids.json` should fail clearly before update work starts.
- Conflict path should print a warning when unrelated staged files exist, because a later manual `git commit` may include them unless the user unstages them.
- If conflict fallback does not create native Git merge state, conflict output/docs must not imply that plain `git commit` will automatically use `.git/MERGE_MSG`; they must give an explicit command such as `git commit -F .git/MERGE_MSG`.

## Quality And Coverage Gates

Required full gate:

```bash
bazel test //...
```

Targeted gates while implementing:

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

Observed local toolchain:

- `bazel --version` succeeded with `bazel 9.1.1`.
- `go version` failed because `go` is not on this shell `PATH`; use Bazel as the required gate.

Coverage requirements:

- Unit tests for scoped cleanliness, unresolved Git operation blocking, temp-index commit behavior, update-all precheck, dirty config blocking, path overlap rejection, conflict behavior, deletes, renames, single-file mirrors, and hook behavior.
- Integration tests for successful scoped update with unrelated staged, unstaged, and untracked files preserved.
- Integration tests for conflict update with unrelated staged entries preserved.
- README docs updated and covered by review.

## Intentional Divergences

- `update` will diverge from `add` and `remove`: only update gets scoped cleanliness.
- `merge-tree --write-tree` replaces `merge-recursive` for update's core merge operation because it supports tree-level merging without touching the user's real index/worktree.
- Proper unmerged index stages are preferred but not mandatory if the explicit conflict-marker fallback semantics above are implemented and documented.

## Review Round 1 Adjustments

- Added an explicit global block for unresolved Git operation state before scoped update work.
- Tightened update-all ordering so all scoped checks happen before fetch, remote setup, cache mutation, worktree writes, or commits.
- Specified the temporary-index commit sequence and required delete/rename/single-file coverage.
- Replaced open-ended conflict fallback language with exact fallback status and manual-resolution semantics.
- Added README recovery requirements so conflicted-update abandon instructions do not rely on broad `git reset --hard` when unrelated work exists.
- Added direct post-commit hook coverage for the temporary-index commit path.
- Added an explicit conflict fallback commit-message contract so marker-file fallback does not depend on plain `git commit` discovering `.git/MERGE_MSG`.

## Open Questions Register

All questions are resolved from the grill-me session. The plan is still pending review and must not be marked accepted until user review feedback is applied.

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | What does scoped unchanged mean? | Update currently checks whole worktree. | Clean index+worktree for scoped paths, or weaker tracked-only check. | Strong check protects staged, unstaged, and deleted state. | Require scoped paths clean in index and worktree. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-02 | resolved | Should update-all check all eligible mirrors up front? | Current update-all can make partial progress. | Up-front scoped check, or check per mirror. | Up-front check prevents partial updates caused by dirty later mirrors. | Check all eligible mirrors before commits. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-03 | resolved | How should automatic commits preserve unrelated index state? | Normal `git add`/`commit` would include unrelated staged entries. | Temporary index, or save/restore real index. | Temporary index is cleaner around partial staging. | Use isolated index for automatic update commits. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-04 | resolved | What should happen on conflicts? | Conflict detection happens after most update work. | Allow conflicts with preserved unrelated state, or block earlier. | Allowing conflicts fits scoped safety but needs warning. | Allow and preserve unrelated state; warn about later manual commits. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-05 | resolved | Do untracked files under the mirror path block update? | Untracked target files could be overwritten or deleted. | Block, or allow. | Blocking matches current global `git status --porcelain` safety. | Block scoped untracked files; ignored files remain allowed. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-06 | resolved | Should add/remove also get relaxed cleanliness? | User request is specific to update. | Update-only, or broaden to add/remove. | Broader change needs separate design. | Update-only. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-07 | resolved | Should no-op updates be allowed with unrelated dirty state? | Up-to-date checks should not be blocked by unrelated work. | Allow no-op, or require clean worktree anyway. | Allowing no-op improves usability and preserves state. | Allow if scoped paths are clean. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-08 | resolved | What is the postcondition after successful update? | Need explicit preservation contract. | Preserve unrelated status exactly, or best-effort. | Exact preservation is testable and safe. | Unrelated changes exactly as before; scoped paths clean at `HEAD`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-09 | resolved | Should update-all keep one commit per mirror? | Current history shape is one update commit per mirror. | Preserve, or batch. | Preserve avoids history behavior change. | Keep one commit per updated mirror. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-10 | resolved | Should scoped errors name the path? | Generic error becomes misleading once unrelated changes are allowed. | Name path, or keep generic. | Naming path improves actionability. | Use `local changes are present in <path>`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-11 | resolved | Should README be updated? | README says add/update/remove require a clean working tree. | Update docs, or leave stale. | Stale docs would contradict behavior. | Update README. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-12 | resolved | Should commits use temporary-index `git commit --no-verify` instead of `commit-tree`? | Probe showed `commit-tree` skips `post-commit`. | Temp-index commit, or commit-tree. | Temp-index commit preserves current commit behavior. | Use `git commit --no-verify` with `GIT_INDEX_FILE`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-13 | resolved | Should update switch to `merge-tree --write-tree`? | Probe showed `merge-recursive` refuses unrelated staged entries. | `merge-tree --write-tree`, or fallback engines. | Tree-level merge avoids touching real index/worktree. | Use `merge-tree --write-tree`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-14 | resolved | Should conflict update advance `.braids.json` immediately? | Current conflict flow writes updated config and merge message. | Preserve current behavior, or keep old revision until resolved. | Advancing config matches existing workflow. | Advance config immediately. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-15 | resolved | Should conflict state create unmerged index stages? | `merge-tree` provides conflict metadata but stage recreation is more complex. | Require stages, or accept conflict-marker fallback. | Stages are closer to Git; fallback keeps scope manageable. | Prefer stages if practical; minimum is markers plus staged config. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-16 | resolved | If stages are not practical, should `.braids.json` still be staged? | The resolution commit should include the config update. | Stage config, or leave all unstaged. | Staging config preserves current workflow. | Stage `.braids.json` at new revision. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-17 | resolved | Must single-file mirrors use the same machinery? | Existing tests support single-file mirrors. | Generic pathspecs, or directory assumptions. | Generic pathspecs prevent regressions. | Support directory and single-file paths via `-- <path>`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-18 | resolved | Should `.braids.json` mirror overlap be rejected? | Metadata and mirror payload cannot share a path. | Reject, or support ambiguity. | Rejecting keeps design simple and safe. | Reject overlaps with `.braids.json`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-19 | resolved | Should dirty-path errors report first path or all paths? | Fail-fast style is common in this codebase. | First path, or aggregate all. | First path is simpler and actionable. | Report first offending scoped path. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-20 | resolved | Should update remove the global preflight clean check? | Preflight cannot know target mirror paths cleanly. | Move to `UpdateHandler`, or keep global clean. | Handler-specific check supports scoped behavior. | Remove update `Clean: true`; enforce scoped check in update handler. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-21 | resolved | Should `update <path>` ignore dirty other mirrors? | Path-scoped intent implies only target path matters. | Ignore other mirrors, or block on any mirror. | Ignoring other mirrors improves utility. | Check only target mirror plus `.braids.json`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-22 | resolved | Should update-all skip locked mirrors before cleanliness checks? | Current update-all skips locked mirrors. | Skip first, or check all mirrors. | Skip first avoids irrelevant blockers. | Check only eligible branch/tag mirrors. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-23 | resolved | Should tracked deletions under the mirror path block update? | Deletions are local changes. | Block, or allow. | Blocking matches scoped safety. | Block deleted tracked files. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-24 | resolved | Should `--keep` remote behavior remain unchanged? | Feature is about worktree/index safety. | Preserve, or refactor remote lifecycle. | Preserve avoids unrelated behavior change. | Preserve current cleanup behavior. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-25 | resolved | Should automatic commits continue using `--no-verify`? | Existing tests assert no-verify. | Preserve, or run hooks. | Preserve avoids hook behavior change. | Continue `--no-verify`. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-26 | resolved | Should unrelated unstaged tracked edits be preserved? | Relaxed update must not touch unrelated working tree edits. | Preserve exactly, or best-effort. | Exact preservation is testable. | Preserve exactly. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-27 | resolved | Should unrelated untracked files be preserved? | Avoid accidental broad add/restore/cleanup. | Preserve, or ignore. | Preserving prevents data loss and regressions. | Preserve exactly. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-28 | resolved | Should restore failures after commit trigger automatic hard rollback? | Broad rollback is dangerous with unrelated work allowed. | No hard rollback, or reset on failure. | No hard rollback avoids destroying unrelated state. | Avoid broad rollback; return clear narrow error. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-29 | resolved | Should implementation add fallback for missing `merge-tree --write-tree`? | Existing minimum Git is 2.39.0. | Require existing minimum support, or add fallback. | No fallback keeps design simple. | Rely on existing minimum Git version. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-30 | resolved | Should tests cover dirty `.braids.json` blocking update? | Config is a scoped path. | Add test, or rely on generic status test. | Dedicated test prevents regression. | Add focused unit and integration coverage as appropriate. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-31 | resolved | Should update-all test blocking before any commits? | Up-front check is a key behavior. | Add test, or rely on unit helpers. | Test protects against partial updates. | Add coverage that no mirror revisions change. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-32 | resolved | Should integration include relaxed-success contract? | Need binary-level proof of real Git behavior. | Add focused integration test, or unit-only. | Integration catches plumbing mistakes. | Add integration success test with staged, unstaged, and untracked unrelated files. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-33 | resolved | Should integration include conflict with unrelated staged changes? | Conflict branch is high risk. | Add integration test, or unit-only. | Integration validates real conflict output and index preservation. | Add integration conflict test. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-34 | resolved | Should grilling stop and become an implementation plan? | Product decisions were settled. | Continue questions, or write plan. | Planning now avoids unnecessary delay. | Stop grilling and emit plan. | User requested structured delivery plan. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |

## Plan Acceptance

This plan was accepted by the user on 2026-06-20. Implementation is delegated to a separate Codex thread.
