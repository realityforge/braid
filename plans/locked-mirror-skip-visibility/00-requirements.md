# Locked Mirror Skip Visibility Requirements

Status: accepted
Date: 2026-06-21

## Mission

Reduce user surprise around no-path commands skipping revision-locked mirrors while preserving the clean target-selection model already implemented by `braid update` and `braid sync`.

## Evidence From Current Code

- `mirror.Mirror.Locked()` returns true when a mirror has no branch and no tag.
- `config.Config.Paths()` returns mirror paths in lexicographic order.
- `UpdateHandler.updateAll` builds a no-path target list in lexicographic mirror path order and skips locked mirrors before scoped cleanliness checks.
- `SyncHandler.syncTargets` does the same for no-path sync.
- Explicit `braid update <path>` and `braid sync --pull-only <path>` can name a locked mirror and follow the existing explicit-path update behavior.
- README already states that no-path update and sync skip revision-locked mirrors, but the successful command output is silent.

## Scope Boundaries

In scope:

- Clarify no-path locked-mirror behavior for `braid update`.
- Keep `braid sync` behavior aligned with no-path update.
- Update tests and README if command behavior or diagnostics change.
- Preserve the existing rule that no-path commands target branch and tag mirrors only.

Out of scope:

- Changing revision-locked mirrors into automatic no-path update targets.
- Changing what counts as a locked mirror.
- Adding fallback behavior for unavailable revisions or remotes.
- Changing explicit-path update semantics.
- Broad refactors outside update/sync target selection or diagnostics.

## Locked Decisions And Non-Negotiables

- No-path `braid update` and `braid sync` must not require locked mirror paths to be clean.
- No-path `braid update` and `braid sync` must not fetch, update, or commit locked mirrors.
- Explicit paths remain the way to act on a locked mirror.
- The implementation must follow existing command/output style and keep tests hermetic.
- Product code that invokes Git must stay behind `internal/gitexec`.

## Command Surface And Behavior Expectations

Current behavior:

- `braid update` targets branch/tag mirrors in lexicographic mirror path order and silently skips revision-locked mirrors.
- `braid sync` and `braid sync --pull-only` with no paths target branch/tag mirrors in lexicographic mirror path order and silently skip revision-locked mirrors.
- Explicit `braid sync --pull-only vendor/revision` is accepted and no-ops when the recorded revision is unchanged.

Expected behavior:

- Preserve the target set above.
- For no-path `braid update`, no-path `braid sync`, and no-path `braid sync --pull-only`, print skipped revision-locked mirrors only after the command returns nil.
- Do not print the skip note for explicit-path commands, including explicit `braid sync --pull-only <locked_path>`.
- Do not print the skip note when no revision-locked mirrors are skipped.
- If all configured mirrors are revision-locked, the no-path command still succeeds without fetching or updating and prints the skipped mirror list.
- Suppress the skip note when the command returns an error, including scoped precheck failures, push-plan failures, fetch failures, and update failures.
- If update conflict materialization returns nil, print the skip note after existing conflict output.
- Print the note to stdout with this exact format, using lexicographic mirror path order and one path per line:

```text
Braid: skipped revision-locked mirrors:
  vendor/a
  vendor/z
```

## Quality And Coverage Gates

Required full gate:

```bash
bazel test //...
```

Targeted gates while implementing:

```bash
bazel test //internal/command:command_test
```

## Open Questions Register

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | What user-visible change should no-path commands make when they skip revision-locked mirrors? | The code already skips locked mirrors before precheck. The UX issue is silent surprise: users may believe a no-path command covered every mirror. | A: print a concise note listing skipped locked mirror paths when any are skipped; B: print only a count; C: documentation/tests only; D: include locked mirrors as explicit no-op targets. | A is clearest but adds successful-command output; B is less noisy but less actionable; C avoids output churn but leaves the surprise in interactive use; D removes asymmetry but adds useless setup/fetch work and makes no-path target semantics less clean. | A: print a deterministic concise note listing skipped paths for no-path `update` and no-path `sync`, preserving skip behavior. | Print the exact stdout skip note defined above after successful no-path `update`, no-path `sync`, and no-path `sync --pull-only`; preserve skip behavior and keep explicit paths quiet. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
