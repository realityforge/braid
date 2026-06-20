# Scoped Add/Remove Cleanliness Test Strategy

Status: implemented
Date: 2026-06-20

## Goals

- Prove add/remove allow unrelated local work while protecting command-owned
  paths.
- Prove unrelated staged entries are not included in automatic Braid commits.
- Prove scoped blockers prevent data loss and config overwrites.
- Keep integration coverage broad enough to exercise real Git behavior without
  duplicating every unit branch.

## Targeted Commands

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

## Full Gate

```bash
bazel test //...
```

## Unit Coverage

### Git Plumbing

- Hash in-memory bytes with Git and verify the resulting blob content.
- Compose a tree with a path removed from a base tree.
- Verify synthetic tree helpers do not alter the real index or worktree.

### Preflight And Scoped Helpers

- Shared scoped helper can be introduced without changing add/remove global
  clean behavior before command rework.
- Add no longer requires global clean preflight only after add has scoped checks
  and temporary-index commit flow.
- Remove no longer requires global clean preflight only after remove has scoped
  checks and temporary-index commit flow.
- The old `Clean` preflight field is removed after both add and remove no longer
  use it.
- Scoped helper ignores unrelated dirty paths.
- Scoped helper reports `local changes are present in <path>` for dirty
  command-owned paths.
- Scoped helper blocks unresolved Git operation states.
- Existing update scoped tests continue to pass.

### `braid add`

- Success with unrelated staged, unstaged tracked, and untracked files:
  - Braid commit changes only `.braids.json` and the mirror target.
  - Unrelated staged blob remains staged and unchanged.
  - Unrelated unstaged tracked edit remains in the worktree.
  - Unrelated untracked file remains present.
- First add works with no existing `.braids.json`.
- Existing dirty `.braids.json` blocks add.
- Target path with clean tracked content in `HEAD` blocks add.
- Target path with tracked content below the target blocks add.
- Target path with tracked, indexed, or worktree file/symlink content at a
  blocking ancestor blocks add.
- Target path with untracked content blocks add.
- Target path overlapping `.braids.json` blocks add.
- Unresolved Git operation state blocks add.
- Failure before commit does not leave temporary remotes or command-owned partial
  state when cleanup succeeds.

### `braid remove`

- Success with unrelated staged, unstaged tracked, and untracked files:
  - Braid commit changes only `.braids.json` and the removed mirror path.
  - Unrelated staged blob remains staged and unchanged.
  - Unrelated unstaged tracked edit remains in the worktree.
  - Unrelated untracked file remains present.
- Dirty `.braids.json` blocks remove.
- Staged mirror change blocks remove.
- Unstaged mirror change blocks remove.
- Missing tracked mirror content blocks remove as local deletion.
- Untracked file under the mirror path blocks remove.
- Unresolved Git operation state blocks remove.
- `--keep` still preserves the remote.
- Non-`--keep` removes delete the Braid-managed remote after commit and narrow
  restore.

## Integration Coverage

### Add Preservation

- Build an upstream and downstream repository through the executable.
- Stage an unrelated file, dirty an unrelated tracked file, and create an
  unrelated untracked file before `braid add`.
- Run `braid add`.
- Assert:
  - exit code is 0;
  - mirror content and `.braids.json` are present;
  - latest commit subject is the add subject;
  - latest commit changed paths exclude unrelated files;
  - staged unrelated blob is unchanged;
  - unstaged and untracked unrelated files remain;
  - porcelain status contains only the unrelated changes.

### Remove Preservation

- Add a mirror, then create unrelated staged, unstaged tracked, and untracked
  files before `braid remove`.
- Run `braid remove`.
- Assert:
  - exit code is 0;
  - mirror path is removed and config no longer contains the mirror;
  - latest commit subject is the remove subject;
  - latest commit changed paths exclude unrelated files;
  - staged unrelated blob is unchanged;
  - unstaged and untracked unrelated files remain;
  - porcelain status contains only the unrelated changes.

### Scoped Blockers

- Cover representative binary-level failures:
  - add blocks dirty `.braids.json`;
  - add blocks existing target path content;
  - remove blocks dirty mirror path;
  - remove blocks untracked content under the mirror path.
- Assert failure diagnostics name the command-owned path.
- Assert unrelated staged/unstaged/untracked files remain after failure when
  present in the fixture.

## README Verification

- README command summary no longer says add/remove require a whole clean
  worktree.
- README describes automatic-commit scoped cleanliness once.
- README lists add/update/remove scopes without repeated long caveats.
- README states unrelated staged, unstaged, and untracked work is preserved and
  excluded from automatic Braid commits.

## Evidence Recording

- Record every targeted and full gate in `20-task-board.yaml`.
- Record commit hashes in completed task entries after commits exist.
- Do not mark completion until `bazel test //...` has passed.
