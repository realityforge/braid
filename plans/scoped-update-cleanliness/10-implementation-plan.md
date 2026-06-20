# Scoped Update Cleanliness Implementation Plan

Status: accepted
Date: 2026-06-20

## Phase Sequence

1. Planning approval
   - Review `00-requirements.md`, this plan, `20-task-board.yaml`, and `40-test-strategy.md`.
   - Apply review feedback before implementation.
2. Git plumbing primitives
   - Add scoped status helpers for path-specific porcelain checks.
   - Add detection for unmerged index entries and in-progress Git operation sentinels.
   - Add gitexec support for `merge-tree --write-tree`.
   - Add temporary-index commit support that preserves `git commit --no-verify` behavior.
   - Add narrow path restore helpers that operate only on explicit pathspecs.
3. Update scoped preflight
   - Remove update from global `Clean` requirements.
   - Load update targets inside `UpdateHandler`.
   - Reject `.braids.json` mirror path overlaps.
   - Reject unresolved Git operation state before scoped path checks.
   - Enforce scoped cleanliness before fetching/updating.
4. Non-conflicting update flow
   - Build base/local/remote trees without real-index mutation.
   - Merge with `merge-tree --write-tree`.
   - Build the post-update tree with updated `.braids.json`.
   - Seed a temporary index from current `HEAD`.
   - Apply only the merged mirror content and updated `.braids.json` into the temporary index, preserving deletes and renames.
   - Commit from that temporary index with `git commit --no-verify`.
   - Restore only the mirror path and `.braids.json` into the real index/worktree from the new `HEAD`.
5. Conflict update flow
   - Materialize conflict-marker files only under the mirror path.
   - Update and stage `.braids.json` at the new revision.
   - Write `.git/MERGE_MSG`.
   - Preserve unrelated index/worktree state.
   - Prefer proper unmerged index stages if practical.
   - If using the accepted fallback, leave conflicted mirror files unstaged with conflict markers, stage only `.braids.json`, create no Braid-generated unmerged index entries, print conflict/warning output, and document the manual resolution flow including `git commit -F .git/MERGE_MSG` or an equivalent command that consumes the generated message.
6. Update-all sequencing
   - Build eligible mirror list first, excluding locked mirrors.
   - Run unresolved-operation, overlap, and scoped cleanliness checks for `.braids.json` and all eligible mirrors before any fetch, setup, remote add/remove, cache mutation, worktree write, or commit.
   - Keep one commit per updated mirror.
7. Documentation
   - Update README text that currently says `add`, `update`, and `remove` all require a clean worktree.
   - Document update's scoped clean requirement and unrelated-state preservation.
   - Replace or qualify conflicted-update recovery guidance so broad `git reset --hard` is not presented as safe when unrelated work exists.
   - If conflict fallback does not create native merge state, document explicit commit-message usage rather than relying on plain `git commit` to use `.git/MERGE_MSG`.
8. Validation and review
   - Run targeted Bazel tests during implementation.
   - Run full `bazel test //...` before completion.
   - Record evidence in `20-task-board.yaml`.

## Delivery Approach

- Execute one task at a time with minimal diffs.
- Keep commits aligned with task boundaries.
- Use existing package boundaries:
  - Git command wrappers in `internal/gitexec`.
  - Command behavior in `internal/command`.
  - User docs in `README.md`.
  - Unit tests under `internal/...`.
  - Binary-level behavior under `integration/...`.
- Do not add fallback merge engines or compatibility shims.
- Do not broaden scope to `add` or `remove`.

## High-Risk Areas

- Preserving partial staging
  - Impact: unrelated user work could be committed, unstaged, or overwritten.
  - Mitigation: temporary indexes for automatic commits; integration tests compare pre/post status and staged blobs.
- Conflict materialization
  - Impact: update conflicts could lose Git-native resolution semantics or include unrelated staged files in manual commits.
  - Mitigation: tree-level merge; scoped materialization; explicit fallback status semantics; warning when unrelated staged entries exist; tests for conflict markers, staged config, `MERGE_MSG`, and unrelated staged blob preservation.
- Update-all partial progress
  - Impact: update-all could fetch, mutate remotes/cache, write files, or commit earlier mirrors before discovering later scoped dirtiness.
  - Mitigation: build target list and run unresolved-operation, overlap, and scoped checks before any update side effect.
- Existing Git operation state
  - Impact: relaxing global cleanliness could allow update to overwrite or confuse an existing merge, rebase, cherry-pick, or revert state.
  - Mitigation: block update when unmerged index entries or Git operation sentinel files are present.
- `.braids.json` path overlap
  - Impact: metadata and payload could collide.
  - Mitigation: reject overlaps before update work starts.
- Hook semantics
  - Impact: replacing `git commit` with `commit-tree` would skip `post-commit`.
  - Mitigation: use `git commit --no-verify` with a temporary index and test that `post-commit` still runs while `pre-commit` is skipped.

## Required Full Gate

```bash
bazel test //...
```

## Targeted Gates

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

## Decision Outcomes

- All grill-me questions Q-01 through Q-34 are resolved.
- No behavior question remains open.
- Plan was accepted by the user on 2026-06-20.

## Decision Log

| id | Plan change |
| --- | --- |
| Q-01 | Scoped cleanliness requires target paths clean in both index and working tree. |
| Q-02 | Update-all performs a full scoped precheck before any mirror commit. |
| Q-03 | Automatic update commits use isolated index state so unrelated staged entries are excluded. |
| Q-04 | Conflict updates are allowed while preserving unrelated state and warning about later manual commits. |
| Q-05 | Untracked files under scoped mirror paths block update; ignored files do not. |
| Q-06 | Scope remains update-only; add/remove retain global clean checks. |
| Q-07 | Up-to-date updates are allowed with unrelated dirty paths and leave state untouched. |
| Q-08 | Successful updates leave unrelated staged, unstaged, and untracked state exactly as before. |
| Q-09 | Update-all keeps one commit per updated mirror. |
| Q-10 | Scoped cleanliness errors name the offending path. |
| Q-11 | README must reflect the new update-only scoped clean behavior. |
| Q-12 | Use `git commit --no-verify` with `GIT_INDEX_FILE`, not `commit-tree`. |
| Q-13 | Replace update's core `merge-recursive` flow with `merge-tree --write-tree`. |
| Q-14 | Conflict updates advance `.braids.json` to the new revision immediately. |
| Q-15 | Prefer unmerged index stages for conflicts; conflict markers plus staged config are acceptable minimum. |
| Q-16 | Stage `.braids.json` in conflict fallback. |
| Q-17 | Treat mirror paths as generic pathspecs; support directory and single-file mirrors. |
| Q-18 | Reject mirror paths that overlap `.braids.json`. |
| Q-19 | Report the first offending scoped path. |
| Q-20 | Remove update's global `Clean` preflight and enforce scoped checks in `UpdateHandler`. |
| Q-21 | `update <path>` checks only `.braids.json` and the target mirror path. |
| Q-22 | Update-all skips locked mirrors before scoped checks. |
| Q-23 | Tracked deletions under scoped mirror paths block update. |
| Q-24 | Preserve current `--keep` remote behavior. |
| Q-25 | Preserve `--no-verify` for automatic update commits. |
| Q-26 | Preserve unrelated unstaged tracked edits. |
| Q-27 | Preserve unrelated untracked files. |
| Q-28 | Avoid broad hard rollback after `HEAD` moves; return narrow path errors. |
| Q-29 | Rely on existing minimum Git support for `merge-tree --write-tree`; no fallback engine. |
| Q-30 | Add tests for dirty `.braids.json` blocking update. |
| Q-31 | Add tests that update-all blocks before any commits if an eligible mirror path is dirty. |
| Q-32 | Add integration success test for unrelated staged, unstaged, and untracked state preservation. |
| Q-33 | Add integration conflict test for unrelated staged preservation. |
| Q-34 | Stop grilling and emit this structured plan for review. |

## Plan Review Finding Log

| round | finding | assessment | plan change |
| --- | --- | --- | --- |
| R1 | Existing Git operation state is not bounded. | Valid. | Added unresolved-operation blocking requirements, sequencing, and tests. |
| R1 | Temporary-index commit pipeline is underspecified. | Valid. | Added exact temp-index sequence and delete/rename/single-file coverage. |
| R1 | Conflict semantics are left too open. | Partially valid because Q-15 allowed a fallback, but the fallback needed exact semantics. | Defined fallback status/output/manual-resolution requirements. |
| R1 | Update-all side-effect boundary is ambiguous. | Valid. | Tightened update-all precheck to occur before fetch, setup, remote, cache, worktree, or commit side effects. |
| R1 | README conflict recovery would become unsafe. | Valid. | Added README requirement to replace or qualify broad `git reset --hard` recovery guidance. |
| R1 | Hook preservation needs direct coverage. | Valid. | Added post-commit coverage requirement for temporary-index commits. |
| R2 | Conflict fallback commit message is underspecified. | Valid. | Required fallback docs/output/tests to use `git commit -F .git/MERGE_MSG` or equivalent unless native merge state is created. |

## Completion Criteria

- User has reviewed and accepted the plan.
- All implementation tasks in `20-task-board.yaml` are completed.
- README reflects final behavior.
- Unit and integration tests cover the agreed behavior.
- `bazel test //...` passes.
- Task evidence is recorded.
- Working tree is clean or any exception is explicitly documented.
