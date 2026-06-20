# Braid Sync Command Test Strategy

Status: accepted
Date: 2026-06-21

## Required Gates

Full gate:

```bash
bazel test //...
```

Targeted gates:

```bash
bazel test //internal/cli:cli_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

## CLI Unit Coverage

`internal/cli/cli_test.go`:

- Parse `sync` with no paths.
- Parse `sync vendor/a vendor/b`.
- Parse `sync --pull-only`.
- Parse `sync --keep`.
- Parse `sync vendor/a --pull-only --keep`.
- Normalize backslashes in sync local path arguments.
- Reject unknown sync flags.
- Include sync in top-level usage.
- Include `usage: braid sync [local_path...] [--pull-only] [--keep]` in command
  usage.

## Command Unit Coverage

`internal/command/sync_test.go`:

- No-path sync selects branch/tag mirrors in sorted config order and skips
  revision-locked mirrors.
- Explicit sync preserves user-provided order.
- Explicit duplicate paths fail after normalization.
- Explicit missing path fails with existing required-mirror diagnostic.
- Mirror path overlap with `.braids.json` fails before side effects.
- Dirty `.braids.json` blocks sync before side effects.
- Dirty selected mirror path blocks sync before side effects.
- Dirty non-selected mirror path does not block explicit sync.
- No-path sync prechecks all selected mirrors before any push/update side effect.
- `--pull-only` still runs the same up-front precheck.
- Fresh-clone default sync hydrates missing recorded revision objects after
  scoped precheck and before push-plan validation.
- If hydration cannot make a selected recorded revision available, default sync
  fails with a clear diagnostic before push planning proceeds.
- Default sync builds and validates the full push plan before any upstream push,
  editor, worktree, config, or commit side effect.
- A later selected tag/revision mirror with committed local changes prevents an
  earlier branch mirror from being pushed.
- Default sync pushes changed branch mirrors, skips unchanged branch mirrors
  quietly, and then updates selected mirrors.
- Branch mirror with no committed local changes and moved upstream updates
  normally instead of failing during push planning.
- Changed branch mirror added with `--path <subdir>` is classified as changed
  by comparing local mirror content with the recorded upstream subdirectory.
- Unchanged branch mirror added with `--path <subdir>` is skipped quietly in the
  push phase.
- Changed single-file branch mirror added with `--path <file>` is classified as
  changed by comparing local mirror file content with the recorded upstream blob.
- Unchanged single-file branch mirror added with `--path <file>` is skipped
  quietly in the push phase.
- Successfully pushed branch mirror has `.braids.json` advanced by the update
  phase.
- `--pull-only` updates selected mirrors without invoking push planning or
  editor.
- Changed branch mirror with moved upstream fails hard and does not run pull
  phase.
- A later changed branch mirror with moved upstream fails push-plan validation
  before any earlier changed branch mirror is pushed or opens an editor.
- Selected tag mirror with committed local changes fails in default sync,
  including no-path tag mirrors.
- Selected revision-locked mirror with committed local changes fails in default
  sync.
- Selected directory mirror path deleted from downstream `HEAD` fails in default
  sync with a clear diagnostic.
- Selected single-file mirror path deleted from downstream `HEAD` fails in
  default sync with a clear diagnostic.
- Explicit revision-locked mirror with `--pull-only` follows update behavior.
- Sync stops on first operational failure and does not process later mirrors.
- Default sync removes temporary Braid remotes.
- Sync `--keep` retains temporary Braid remotes.

`internal/command/push_test.go`:

- Existing `braid push` no-local-changes output and success behavior remain
  unchanged after push-status refactor.
- Existing `braid push` not-up-to-date output and success behavior remain
  unchanged after push-status refactor.
- Existing normal push, explicit branch push, editor stdin, editor failure, and
  identity propagation behavior remain covered.

`internal/command/preflight_test.go`:

- `RequirementsFor(cli.CommandSync)` requires Git root and config.
- `RequirementsFor(cli.CommandSync)` includes `MayWrite: true`.
- Sync does not require global clean preflight; scoped cleanliness is owned by
  `SyncHandler`.

## Integration Coverage

`integration/braid_integration_test.go`:

- Default sync push then update:
  - Create upstream/downstream repos.
  - Add a branch-tracking mirror.
  - Commit local downstream mirror changes.
  - Run `braid sync <path>` with scripted editor.
  - Assert upstream content changed.
  - Assert upstream commit subject came from editor.
  - Assert downstream `.braids.json` records the pushed upstream revision.
  - Assert temporary remote cleanup matches default behavior.
- Fresh-clone sync:
  - Clone or reconstruct a downstream state where `.braids.json` and mirror
    files are present but recorded mirror revision objects are missing locally.
  - Run default sync.
  - Assert sync hydrates the recorded revision objects before push planning.
  - Assert no editor or upstream push occurs unless committed local mirror
    changes are present.
- Pull-only sync:
  - Create upstream/downstream repos.
  - Add a branch-tracking mirror.
  - Change upstream.
  - Run `braid sync --pull-only <path>` with an editor that would fail if
    invoked.
  - Assert downstream mirror content and `.braids.json` update.
  - Assert upstream was not modified by downstream local changes.
- Repository-wide sync:
  - Create at least two branch/tag mirrors.
  - Ensure no-path sync processes in sorted config order where observable.
  - Assert locked mirror is skipped in no-path mode.
  - Assert a tag mirror with committed local changes fails before any branch
    mirror is pushed.
- Precheck side-effect boundary:
  - Create at least two selected mirrors.
  - Make the later selected mirror path dirty.
  - Make the earlier selected branch mirror eligible for push or update.
  - Run sync and assert failure names the dirty later mirror.
  - Assert no object hydration, upstream push, downstream update commit, Braid
    remote, or config revision change occurred before the scoped precheck
    failure.
- Push-plan side-effect boundary:
  - Create a changed branch mirror first in target order and a later selected
    non-branch mirror with committed local changes.
  - Run default sync.
  - Assert failure names the non-branch local-change problem.
  - Assert the earlier branch upstream was not pushed and no editor, worktree,
    config, or commit side effect occurred. Bounded object hydration may have
    occurred if needed for classification.
- Freshness side-effect boundary:
  - Create two changed branch mirrors where the later mirror's upstream moved
    since `.braids.json`.
  - Run default sync.
  - Assert failure names the later stale mirror.
  - Assert the earlier branch upstream was not pushed and no editor was opened.

## README Review Checks

- README command list includes `sync`.
- Quick start mentions `braid sync <path>` as the combined push/update workflow
  without removing the explicit `push` and `update` explanations.
- Pushing section explains that sync auto-pushes branch mirrors only.
- Updating/sync section explains no-path target selection and locked mirror
  behavior.
- Command reference includes `sync [local_path...] [--pull-only] [--keep]`.
- Diagnostics section explains not-up-to-date sync failure and explicit
  tag/revision local-change failure.
- Diagnostics section explains selected mirror-path deletion failure.

## Residual Risk To Review

- Push-status refactor is the highest regression risk because it touches existing
  `push` behavior. Preserve visible `push` semantics with direct tests before
  relying on the refactor from sync.
- No-path sync can include tag mirrors for pull/update but cannot auto-push them.
  The plan intentionally preserves Q-02/Q-09: automatic push is branch-only, and
  selected non-branch local changes fail in default sync.
