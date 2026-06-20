# Scoped Update Cleanliness Test Strategy

Status: accepted
Date: 2026-06-20

## Required Gates

Full gate:

```bash
bazel test //...
```

Targeted gates:

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

## Unit Test Coverage

`internal/gitexec/gitexec_test.go`:

- Path-scoped porcelain status uses explicit pathspecs and ignores unrelated dirty paths.
- Unmerged index entries and in-progress operation sentinel files block update before scoped path checks.
- `merge-tree --write-tree` wrapper returns merged tree on clean merge.
- `merge-tree --write-tree` wrapper returns conflict details without touching real index/worktree.
- Temporary-index commit excludes real-index staged entries and preserves `git commit --no-verify` behavior.
- Temporary-index update commit skips pre-commit hooks but still runs post-commit hooks.
- Narrow restore helper updates only explicit pathspecs.

`internal/command/preflight_test.go`:

- `RequirementsFor(cli.CommandUpdate)` no longer includes `Clean: true`.
- `add` and `remove` still require clean worktree.
- `diff`, `status`, `setup`, and `push` requirements remain unchanged.

`internal/command/update_test.go`:

- `update <path>` succeeds with unrelated staged changes and excludes them from the Braid commit.
- `update <path>` succeeds with unrelated unstaged tracked changes and preserves them.
- `update <path>` succeeds with unrelated untracked files and preserves them.
- `update <path>` blocks dirty `.braids.json`.
- `update <path>` blocks when unmerged entries or operation sentinels such as `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`, `REBASE_HEAD`, `rebase-merge`, or `rebase-apply` exist, even outside the mirror path.
- `update <path>` blocks staged, unstaged, deleted, and untracked changes under the mirror path.
- `update <path>` ignores dirty non-target mirror paths.
- `update` checks unresolved Git operation state and all eligible branch/tag mirror paths before any fetch, setup, remote, cache, worktree, or commit side effect.
- `update` skips locked mirrors before scoped checks.
- Update-all dirty eligible mirror leaves all mirror revisions unchanged.
- Directory mirrors and single-file mirrors use the same scoped path handling.
- Mirror deletes and renames are represented correctly in the Braid update commit and restored scoped worktree.
- Mirror paths overlapping `.braids.json` are rejected.
- Up-to-date update with unrelated dirty state leaves `HEAD`, index, and working tree unchanged.
- Conflict update writes markers, stages `.braids.json`, writes `MERGE_MSG`, and preserves unrelated index state.
- If conflict fallback is used instead of unmerged stages, status is exactly documented: conflicted mirror files are unstaged with markers, `.braids.json` is staged, and no Braid-created unmerged entries exist.
- If conflict fallback does not create native merge state, tests verify output/docs instruct `git commit -F .git/MERGE_MSG` or an equivalent command, and do not imply plain `git commit` automatically uses the generated message.

## Integration Test Coverage

`integration/braid_integration_test.go`:

- Successful scoped update:
  - Create a downstream repo with a Braid mirror.
  - Add unrelated staged file change.
  - Add unrelated unstaged tracked file change.
  - Add unrelated untracked file.
  - Update the mirror.
  - Assert the Braid commit contains only the mirror path and `.braids.json`.
  - Assert the unrelated staged blob is unchanged.
  - Assert unrelated unstaged and untracked files remain as before.
  - Assert final porcelain status matches the expected unrelated state.
- Conflict scoped update:
  - Create a real upstream/downstream conflict.
  - Add unrelated staged file change before update.
  - Run `braid update <path>`.
  - Assert conflict markers are present in the mirror path.
  - Assert `.git/MERGE_MSG` contains the Braid update subject.
  - Assert `.braids.json` is advanced and staged at the new revision.
  - Assert unrelated staged blob remains unchanged.
  - Assert conflict output warns about unrelated staged entries if present.
  - If marker-file fallback is used, assert conflict output gives an explicit commit-message command such as `git commit -F .git/MERGE_MSG`.
- Update-all scoped precheck:
  - Create at least two eligible mirrors.
  - Dirty one eligible mirror path.
  - Update both upstreams.
  - Run `braid update`.
  - Assert the error names the dirty mirror path.
  - Assert neither mirror revision changed and no update commit was created.
  - Assert no Braid remote/cache/worktree side effects occurred before the failure.

## README Review Checks

- README command summary no longer claims update requires a fully clean worktree.
- Update section states `.braids.json` and target mirror paths must be clean.
- Conflict section notes that unrelated staged entries are preserved and may be included in a later manual commit unless unstaged.
- Conflict recovery section does not present `git reset --hard` as safe when unrelated dirty work may exist; it must describe scoped recovery or explicitly warn about unrelated work.
- Conflict fallback instructions explicitly use `git commit -F .git/MERGE_MSG` or equivalent unless Braid creates native merge state.

## Residual Risk To Review

- Whether to implement proper unmerged index stages remains an implementation-time decision. If not implemented, the marker-file fallback has exact required status, output, README, and commit-message semantics.
- If narrow restore fails after `HEAD` moves, the plan intentionally avoids broad hard rollback to protect unrelated user work.
