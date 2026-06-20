# Sync Command Deep Dive

Status: accepted
Date: 2026-06-21

## Feature

- Name: `braid sync`
- Related tasks: `PLAN-APPROVAL`, `T01`, `T02`, `T03`, `T04`, `T05`
- Owner: implementation agent after plan approval

## Problem Statement

Today a user who has local mirror changes must run separate commands:

1. `braid push <path>` to create and push an upstream commit.
2. `braid update <path>` to record the pushed upstream revision in
   `.braids.json`.

For multiple mirrors this becomes repetitive and easy to do partially. `sync`
should provide a coordinated push-then-pull workflow while keeping existing
safety boundaries explicit.

## Inputs And Interfaces

Command:

```bash
braid sync [local_path...] [--pull-only] [--keep]
```

New CLI model:

- `cli.CommandSync`
- `cli.SyncOptions`
  - `LocalPaths []string`
  - `PullOnly bool`
  - `Keep bool`

Parser rules:

- Positionals are 0-N local paths.
- `--pull-only` is boolean.
- `--keep` is boolean.
- Unknown flags fail using existing command parser diagnostics.
- Backslashes in local path args are normalized to slashes.
- Duplicate detection occurs after repository-relative normalization in command
  handling, not just raw parser input.

## Target Selection

No explicit paths:

- Load and validate config.
- Use `cfg.Paths()` sorted order.
- Include branch/tag mirrors only.
- Exclude revision-locked mirrors before precheck.

Explicit paths:

- Normalize each path with existing subdirectory-aware path logic.
- Preserve user-provided order.
- Reject duplicates after normalization.
- Require every named mirror to exist.
- Pull phase includes each named mirror and uses explicit update behavior.

Push phase:

- Disabled entirely by `--pull-only`.
- Only branch-tracking mirrors are automatically pushable.
- Branch-tracking mirrors are pushed only if committed downstream `HEAD` content
  under the mirror path differs from the recorded upstream revision.
- Tag/revision mirrors with committed local changes fail in default sync because
  `sync` has no `--branch`, regardless of whether they were selected explicitly
  or by no-path sync.
- Tag/revision mirrors without committed local changes are not pushed; they may
  still participate in pull/update according to target rules.

## Precheck Contract

Before push or pull work starts:

- Detect unresolved Git operation state through existing `BlockingOperation`.
- Reject mirror paths overlapping `.braids.json`.
- Require `.braids.json` clean.
- Require every selected mirror path clean.
- Check all selected scopes before any fetch, remote setup, remote cleanup, cache
  mutation, worktree write, commit, editor invocation, or push.

The precheck protects working tree/index cleanliness, not committed local mirror
changes. Committed local mirror changes are exactly what the push phase may send.

## Object Hydration Contract

Push planning compares downstream `HEAD` mirror content with the recorded mirror
revision. In a fresh clone or a repository with pruned objects, that recorded
revision may be absent locally.

After scoped precheck and before push-plan validation, default sync may perform
bounded object hydration for selected mirrors:

- Set up or reuse temporary Braid remotes as existing commands do.
- Fetch the selected mirrors or update the local cache only enough to make the
  recorded mirror revisions available for classification.
- Respect `--keep` for temporary remotes created during hydration.
- Do not push upstream, invoke an editor, write the worktree, write
  `.braids.json`, or create commits.
- If the recorded revision remains unavailable after hydration, fail with a
  clear diagnostic before push planning proceeds.

## Push Plan Contract

Default sync must build a complete push plan after scoped precheck and any
required object hydration, before any upstream push, editor, worktree, config, or
commit side effect.

Push planning:

- Classifies committed local mirror-content changes for every selected target
  before checking upstream freshness.
- Treats a mirror as changed when the selected mirror content in downstream
  `HEAD` differs from the configured mirror revision.
- Uses existing remote-path-aware mirror item semantics for classification:
  compare downstream `HEAD` at `m.Path` against the recorded upstream item at
  `m.RemotePath`, or the whole recorded revision when `m.RemotePath` is empty.
  This must handle tree mirrors, subdirectory mirrors, and single-file mirrors
  without comparing unrelated upstream repository content.
- Treats committed deletions inside an existing mirror directory as changed
  content.
- Treats the selected mirror path being absent from downstream `HEAD` as an
  unsupported sync push case and fails with a sync diagnostic.
- Marks changed branch mirrors as push actions.
- Marks unchanged branch mirrors as quiet skips.
- Fails selected tag/revision mirrors with committed local changes before any
  push action.
- Checks upstream freshness for every changed branch mirror before any push
  action or editor invocation. If any changed branch mirror is not up to date,
  the whole push plan fails before earlier changed branch mirrors are pushed.

This ordering prevents an unchanged branch mirror whose upstream moved from
being misreported as a failed push. It also prevents earlier mirrors from being
pushed before a later selected mirror is known to be unpushable or not up to
date.

## Phase Semantics

Default `sync`:

1. Precheck selected target scopes.
2. Hydrate missing recorded revision objects for selected mirrors if needed,
   without upstream push, editor, worktree, config, or commit side effects.
3. Build and validate the full push plan for selected targets.
4. For each planned push action in target order:
   - Push changed branch mirrors via existing push machinery.
   - Skip unchanged branch mirrors quietly.
5. For each selected pull target in order:
   - Update using existing update behavior.

`sync --pull-only`:

1. Precheck selected target scopes.
2. Skip push planning, push execution, and editor invocation.
3. Update selected pull targets in order.

## Error Handling

Stop-on-first-failure:

- After the up-front precheck, any required object hydration, and default-sync
  push-plan validation pass, any push or update error stops the command.
- Later mirrors and later phases are skipped after the first operational failure.

Upstream moved:

- Existing single-mirror push behavior prints a stop message and returns success.
- `sync` must convert that state into a hard error when committed local changes
  exist.
- The error should name the mirror and tell the user to update, resolve,
  commit, and retry.

Non-pushable mirror with local changes:

- Tag/revision mirrors with local committed changes fail in default sync,
  including mirrors selected by no-path sync.
- Diagnostic should point to `braid push <path> --branch <branch>` for explicit
  push intent or `braid sync --pull-only <path>` for pull-only intent.

Mirror path removed:

- If downstream `HEAD` has no item at the selected mirror path, default sync
  fails push planning with a clear diagnostic.
- The command may still support ordinary file deletes inside an existing mirror
  directory because the mirror path itself remains present as a tree.

## Implementation Shape

Expected refactors:

- Extend push internals to return a small status/sentinel for:
  - pushed,
  - no local changes,
  - not up to date.
- Add or expose object hydration and a committed-local-change classifier used by
  sync push planning before freshness checks.
- Build the classifier from existing `baseDiffItem` / `itemAtRevision` /
  synthetic-tree comparison semantics rather than comparing whole commits.
- Keep `PushHandler.Run` user-visible behavior compatible by mapping existing
  statuses back to the current stdout/success behavior.
- Let `SyncHandler` treat those statuses differently:
  - skip no-local-change status quietly,
  - fail on not-up-to-date status.
- Reuse update's scoped precheck helper or move shared target precheck logic to a
  package-local helper.
- Reuse `UpdateHandler.updateOne` for pull phase to preserve update behavior and
  commit shape.

## Compatibility And Parity

Preserved:

- Existing `push`, `update`, `status`, `diff`, `setup`, `add`, and `remove` CLI
  behavior.
- Existing push editor workflow for actual upstream commits.
- Existing update behavior for explicit paths and no-path eligible mirrors.
- Existing `--keep` semantics for temporary remotes.
- Existing sorted config order for no-path repository-wide commands.

Intentional divergences:

- `sync` hard-fails when a pushable mirror with local committed changes is not up
  to date.
- `sync` skips unchanged push targets quietly.
- `sync` accepts multiple explicit local paths.
- `sync` has no `--branch`.

## Acceptance Criteria

- [ ] CLI parser and usage expose the new command and flags.
- [ ] Target selection follows no-path sorted order and explicit user order.
- [ ] Duplicate explicit paths are rejected after normalization.
- [ ] Up-front precheck occurs before any operational side effects.
- [ ] Default sync can hydrate missing recorded revisions after precheck and
      before push-plan validation.
- [ ] Default sync builds a complete push plan before any upstream push, editor,
      worktree, config, or commit side effect.
- [ ] Push phase pushes changed branch mirrors only.
- [ ] Push phase does not prompt for unchanged mirrors.
- [ ] Push phase hard-fails on upstream-moved local-change state.
- [ ] Push planning validates freshness for all changed branch mirrors before any
      push/editor side effect.
- [ ] Push phase does not hard-fail unchanged branch mirrors just because
      upstream moved.
- [ ] Push planning classifies subdirectory and single-file mirrors using
      `RemotePath` semantics.
- [ ] Push planning fails selected non-branch mirrors with committed local
      changes, including no-path tag mirrors.
- [ ] Push planning fails selected mirror paths absent from downstream `HEAD`
      with a clear diagnostic.
- [ ] Pull phase runs after push phase, including for successfully pushed
      mirrors.
- [ ] `--pull-only` skips push planning and editor invocation.
- [ ] `--keep` applies to both phases.
- [ ] README and tests describe final behavior.

## Open Questions

None. Review feedback may add post-plan issues under this plan tree before
implementation starts.
