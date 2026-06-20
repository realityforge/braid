# Subdirectory Execution Test Strategy

Status: accepted
Date: 2026-06-20

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

## Unit Coverage

`internal/gitexec`:

- Worktree root discovery from root and subdirectory.
- Existing Git-path helpers continue to resolve per-worktree metadata and common object paths.

`internal/command`:

- Preflight allows subdirectory execution and still rejects outside-worktree execution.
- Config root resolves to the worktree root.
- Resolved `RepoContext` distinguishes `ProcessWorkDir`, `GitWorkTreeRoot`, optional `LogicalWorkTreeRoot`, root Git, and process Git.
- Relative `local_path` normalization from nested process directories.
- Omitted `add` default path placement under the process directory.
- Absolute path input inside the worktree.
- Absolute path input through a symlink-spelled worktree path.
- Invocation from an internal symlink directory whose target has a different Git prefix; relative mirror inputs follow Git's prefix, and unsupported absolute symlink-spelled inputs fail clearly.
- Absolute path input outside the worktree.
- Backslash separator normalization before repo-relative validation.
- `.` at a configured mirror root.
- `.` inside a mirror subdirectory does not select the containing mirror.
- Parent traversal that remains inside the worktree.
- Parent traversal that escapes the worktree.
- Logical symlink path behavior.
- Cache path resolution remains process-directory-relative.
- Diff passthrough args remain raw and the final diff uses process-directory Git semantics.
- Directory and single-file mirror diff arg construction from a subdirectory, including `:(top)` for single-file internal pathspecs.
- Conflict instruction paths use the exact process-runnable command shape.

## Integration Coverage

Add binary-level tests using local repositories in `t.TempDir()`:

- Full lifecycle from subdirectory:
  - initialize downstream with nested directory,
  - run `braid add` from nested directory with a relative path,
  - assert `.braids.json` stores the expected repo-root path,
  - run `setup`, `status`, `diff`, `update`, `push`, and `remove` from the same or another subdirectory,
  - assert commits and filesystem changes affect the intended repo-root paths.
- Omitted add path from subdirectory:
  - `braid add <upstream>` from `apps/web`,
  - assert mirror path is `apps/web/<basename>`.
- No-path repository-wide commands:
  - create mirrors under different directories,
  - run `status` or `diff` from one subdirectory,
  - assert all configured mirrors are considered.
- Absolute path input:
  - pass an absolute mirror path inside the worktree and assert it resolves to config path,
  - pass an absolute path through a symlink-spelled worktree path and assert logical normalization,
  - pass an absolute outside path and assert rejection.
- Diff passthrough:
  - run `braid diff <mirror> -- <cwd-relative-pathspec>` from a subdirectory,
  - assert behavior matches raw Git expectation and existing mirror-relative prefixes.
  - cover a single-file mirror and assert the internal top-anchored limiter still produces the expected prefixes.
- Conflict instructions:
  - create an update conflict from a subdirectory,
  - assert printed recovery commands use `:(top)` pathspecs and a process-dir-valid merge message path,
  - resolve conflict markers and execute the printed command shape from the original subdirectory.

## Documentation Review

- README command form removes root-only language.
- Path semantics section states:
  - CLI mirror path inputs are process-directory-relative.
  - Stored config and ordinary output are repo-root-relative.
  - Relative cache paths remain process-directory-relative.
  - Diff args after `--` are passed to Git as-is.
  - No-path commands operate repository-wide.

## Evidence Recording

- Record targeted command results in `20-task-board.yaml` as tasks close.
- Record full `bazel test //...` result before marking implementation complete.
