# Command Parity Deep Dive

Status: completed
Last updated: 2026-06-14

## Feature

- Name: CLI and behavior parity
- Owner: current planning thread
- Related plan/task IDs: T03, T04, T05, T06, T07, T08, T09, T10

## Problem Statement

The Go port must be familiar to existing Braid users without dragging forward Ruby-specific implementation details or historic migration paths. The command layer is the user's contract, so it needs explicit parity boundaries.

## Scope

In scope:

- Current modern commands and flags.
- Current `.braids.json` behavior.
- Current Git side effects: remotes, fetches, index/worktree updates, commits, conflicts, diffs, and pushes.
- Existing branch/tag/revision mirror tracking.
- Subdirectory and single-file mirrors.
- `upgrade-config` as an unknown command.

Out of scope:

- Ruby help/message text parity.
- YAML/PStore upgrade support.
- SVN or full-history mirror migration.
- Supporting command execution outside the Git worktree root in v1.
- Implementing `upgrade-config`.

## Command Requirements

### `add`

- Detect default branch when branch/tag/revision is omitted.
- Support explicit branch, tag, revision, and upstream path.
- Reject tag+branch and tag+revision combinations.
- Add content to the requested local path.
- Write `.braids.json`.
- Commit with a Braid-style add commit message.
- Clean up remotes unless `--keep` behavior applies through shared setup/cleanup logic.

### `update`

- Update one mirror or all eligible mirrors.
- For all-mirror update, eligible means branch- or tag-tracking mirrors; revision-locked mirrors are skipped.
- Reject `--branch`, `--tag`, and `--revision` when no `local_path` is supplied.
- Reject local changes before mutating state.
- Support switching branch/tag/revision.
- Preserve conflict behavior: leave files and index for manual resolution and write merge message.
- Create a merge/update commit when non-conflicting changes exist.
- Do not implement deprecated `--head`; reject it as an unknown flag.

### `remove`

- Remove mirrored files from Git.
- Remove mirror config entry.
- Remove remote unless `--keep`.
- Commit removal.

### `diff`

- Show local changes relative to upstream mirror content.
- Support all mirrors or one mirror.
- Pass arguments after `--` to `git diff` as separate argv entries.
- Avoid shell interpolation.
- Preserve single-file mirror diff behavior.

### `push`

- Push local mirror changes to a branch.
- Reject pushing to a tag without explicit branch.
- Stop when mirror is not up to date.
- Use temporary repository/alternate object database strategy or a simpler equivalent if tests prove identical behavior.
- Preserve interactive `git commit -v` behavior, identity propagation, and no-push-on-cancel/failure behavior.

### `setup`

- Create mirror remotes.
- Reuse existing remotes unless forced.
- Respect local cache decisions.

### `status`

- Report mirror revision and tracking mode.
- Detect remote modification, local modification, and local removal.
- Work for branch, tag, revision-locked, subdirectory, paths with spaces, and single-file mirrors.

### `upgrade-config`

- Intentionally not implemented.
- Must follow generic unknown-command behavior.
- Legacy `.braids` and unsupported config diagnostics belong in normal config loading paths, not in an upgrade command.

## Error Handling and Diagnostics

- Use clear, idiomatic, stable error categories; exact Ruby wording is not required.
- Return non-zero on invalid usage, Git command failure, unsafe path, unsupported config, or incompatible Git version.
- Include the failed Git command's stderr for Git failures where useful.
- Avoid leaking shell-escaped commands as instructions to execute; verbose output may show command argv using a deterministic representation.

## Compatibility and Parity

Baseline source:

- Ruby implementation in `vendor/braid/lib/braid`.
- Ruby fixtures and integration flows in `vendor/braid/spec`.

Intentional divergences:

- No legacy config readers.
- Stricter path validation.
- Git minimum is 2.39.0.
- Deprecated `update --head` is removed.
- Help text and ordinary output may use a new idiomatic Go design.

## Acceptance Criteria

- [x] Each command has focused CLI parser tests.
- [x] Each command has at least one real-Git integration test.
- [x] High-risk commands `add`, `update`, `diff`, and `push` have behavior/golden tests across branch, tag, revision, subdirectory, path-with-space, and single-file cases.
- [x] Error behavior is tested for invalid flags/arguments and unsafe paths.
- [x] Tests assert no command uses shell execution for Git.
- [x] User-approved divergences are documented in `30-compatibility-matrix.md`.

## Open Questions

- No open command parity questions remain.
