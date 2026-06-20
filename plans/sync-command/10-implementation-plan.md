# Braid Sync Command Implementation Plan

Status: accepted
Date: 2026-06-21

## Phase Sequence

1. Planning review
   - Review `00-requirements.md`, `01-sync-command-deep-dive.md`, this plan,
     `20-task-board.yaml`, and `40-test-strategy.md`.
   - Apply review feedback before implementation.
   - Do not mark the plan accepted until user review outcome is recorded.
2. CLI surface
   - Add `cli.CommandSync`, `cli.SyncOptions`, invocation storage, parser rules,
     command usage, and top-level usage text.
   - Add parser tests for zero paths, multiple paths, flags, usage errors, and
     path normalization.
3. Command wiring and target planning
   - Add `SyncHandler` and wire it through `command.NewAppWithOptions`.
   - Add `RequirementsFor(cli.CommandSync)` with Git root, config, and
     `MayWrite`.
   - Load/validate config once for sync.
   - Resolve target lists for no-path and explicit-path modes.
   - Reject duplicate explicit paths after normalization.
   - Reuse existing preflight requirements for Git root and config.
4. Up-front scoped precheck
   - Share or reuse update's scoped-clean helper.
   - Reject `.braids.json` overlaps for every selected target.
   - Run unresolved-operation, `.braids.json`, and selected mirror path checks
     before object hydration or any push/update side effect.
   - Add tests proving later dirty targets block before earlier pushes/updates.
5. Push-phase refactor
   - Add a bounded object-hydration phase for selected mirrors whose recorded
     revisions are missing locally.
   - Add side-effect-free committed-local-change classification for selected
     mirror content in downstream `HEAD` versus the recorded mirror revision.
   - Reuse existing remote-path-aware item/tree semantics so subdirectory and
     single-file mirrors compare `HEAD:m.Path` against the recorded upstream
     item at `m.RemotePath`.
   - Define selected mirror path absence as an unsupported sync push case with a
     clear diagnostic.
   - Build and validate the full push plan after object hydration and before any
     upstream push, editor, worktree, config, or commit side effect.
   - Validate upstream freshness for every changed branch mirror before any
     planned push action or editor invocation.
   - Factor push internals so the core push path can report pushed/no-local-
     changes/not-up-to-date states.
   - Keep `braid push` output and exit behavior unchanged.
   - Let `sync` skip no-local-change mirrors quietly.
   - Let `sync` fail on not-up-to-date local-change mirrors.
   - Reject selected non-branch mirrors with committed local changes, including
     no-path tag mirrors.
6. Pull/update phase
   - Reuse update internals for every selected pull target in order.
   - Ensure `--keep` flows into both push and update options.
   - Ensure pushed mirrors are still updated so `.braids.json` records new
     upstream revisions.
7. Documentation and integration coverage
   - Update README quick start, pushing/updating sections, and command reference.
   - Add integration coverage for default sync and pull-only sync.
   - Run full validation and record evidence.

## Delivery Approach

- Execute one task at a time with minimal diffs.
- Prefer existing package boundaries:
  - CLI parsing in `internal/cli`.
  - Command orchestration in `internal/command`.
  - Git command execution behind `internal/gitexec`.
  - User docs in `README.md`.
  - Binary behavior in `integration`.
- Refactor existing push/update helpers only where it directly supports `sync`.
- Preserve existing command behavior with tests before relying on refactored
  internals.
- Keep planning artifacts aligned with any review-driven scope changes.

## High-Risk Areas

- Push status refactor
  - Impact: existing `braid push` could accidentally change output or exit code.
  - Mitigation: focused push tests for no-local-changes, not-up-to-date, normal
    push, explicit branch, editor failure, and stdin/editor behavior.
- Local-change classification ordering
  - Impact: unchanged branch mirrors whose upstream moved could be misclassified
    as failed pushes.
  - Mitigation: side-effect-free local-change classification before freshness
    checks; tests for unchanged branch mirrors with upstream changes.
- Object availability in fresh clones
  - Impact: strict no-fetch push planning cannot compare against recorded mirror
    revisions that are missing from local object storage.
  - Mitigation: bounded object hydration after scoped precheck and before push
    planning; fresh-clone sync coverage; clear diagnostic if hydration cannot
    make the recorded revision available.
- Full push-plan validation
  - Impact: an earlier mirror could be pushed before a later selected tag or
    revision mirror is found to have unpushable local changes, or before a later
    changed branch mirror is found to be out of date.
  - Mitigation: validate the full push plan before any upstream push, editor,
    worktree, config, or commit side effect; tests assert no upstream push occurs
    when a later target fails push planning or freshness validation.
- Up-front precheck side-effect boundary
  - Impact: sync could push one mirror before discovering another selected mirror
    is dirty.
  - Mitigation: command test with two mirrors where the later target is dirty and
    the earlier target would otherwise push/update.
- Branch/tag/revision target semantics
  - Impact: sync could silently ignore local changes for a mirror the user named.
  - Mitigation: selected non-branch local-change tests for explicit paths and
    no-path tag mirrors, plus README diagnostics.
- Selected mirror path deletion
  - Impact: a low-level `ls-tree` failure could leak to users or implementation
    could accidentally promise upstream path deletion semantics that update
    cannot consistently represent.
  - Mitigation: default sync push planning rejects selected mirror paths absent
    from downstream `HEAD` with a clear diagnostic; tests cover directory and
    single-file mirror path deletion.
- Remote-path-aware classification
  - Impact: sync could falsely classify subdirectory or single-file mirrors by
    comparing against the whole upstream revision instead of the configured
    upstream item.
  - Mitigation: build classification from existing mirror item/tree helpers and
    add sync coverage for changed and unchanged `--path` subdirectory and
    single-file branch mirrors.
- Reusing update after push
  - Impact: pushed upstream revision might not be recorded in `.braids.json`.
  - Mitigation: integration test asserts config revision advances after sync
    pushes.
- Editor prompts in multi-mirror sync
  - Impact: unchanged mirrors could prompt unexpectedly or multiple pushes could
    be hard to reason about.
  - Mitigation: tests with scripted editor; assert unchanged mirrors do not
    invoke editor.
- Remote cleanup
  - Impact: combined phases could leak or remove remotes contrary to `--keep`.
  - Mitigation: tests for default cleanup and `--keep` retention.

## Required Full Gate

```bash
bazel test //...
```

## Targeted Gates

```bash
bazel test //internal/cli:cli_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

## Decision Outcomes

- All grill-me questions Q-01 through Q-12 are resolved.
- No behavior question remains open.
- Plan was accepted by the user on 2026-06-21 by requesting implementation.

## Decision Log

| id | Plan change |
| --- | --- |
| Q-01 | No-path sync uses the same branch/tag target set as no-path update and skips locked mirrors. |
| Q-02 | Sync has no `--branch`; automatic push phase only pushes branch-tracking mirrors, and selected non-branch mirrors with local changes fail. |
| Q-03 | Sync runs all selected scoped prechecks, hydrates missing objects if needed, then validates the full push plan before any upstream push, editor, worktree, config, commit, or pull/update side effect. |
| Q-04 | Sync detects committed local mirror-content changes and skips unchanged mirrors quietly. |
| Q-05 | Sync always runs pull/update after push, including for successfully pushed mirrors. |
| Q-06 | Sync stops on first operational failure after precheck and push-plan validation. |
| Q-07 | Sync treats not-up-to-date push state with local changes as a hard failure. |
| Q-08 | Sync preserves one upstream commit editor invocation per actually pushed mirror. |
| Q-09 | `--pull-only` follows update's target rules; default sync rejects selected non-branch mirrors with local changes. |
| Q-10 | Sync includes `--keep` and applies it to both push and update phases. |
| Q-11 | Explicit paths run in user order and duplicates are rejected after normalization. |
| Q-12 | Planning artifacts are emitted before implementation for review. |

## Implementation Tasks

Tasks are tracked in `20-task-board.yaml`.

- `PLAN-APPROVAL`: review and accept or revise the plan.
- `T01`: add CLI parser and usage.
- `T02`: add sync handler, target planning, and up-front precheck.
- `T03`: refactor push status and implement sync push phase.
- `T04`: implement update phase integration and phase ordering.
- `T05`: update README and integration coverage; run full gate.

## Completion Criteria

- User has reviewed and accepted the plan.
- All planned tasks in `20-task-board.yaml` are completed.
- Existing `push` and `update` behavior remains covered.
- README reflects final `sync` behavior.
- Targeted gates pass during implementation.
- `bazel test //...` passes before completion.
- Task evidence is recorded.
- Working tree is clean or any exception is explicitly documented.

## Plan Review Finding Log

| round | finding | assessment | plan change |
| --- | --- | --- | --- |
| R1 | Local-change classification is underspecified. | Valid. | Added side-effect-free local-change classification before freshness checks and coverage for unchanged branch mirrors with upstream movement. |
| R1 | Non-branch local changes are inconsistent for no-path sync. | Valid. | Required any selected non-branch mirror with committed local changes to fail default sync, including no-path tag mirrors. |
| R1 | Later push-plan failures can occur after earlier pushes. | Valid. | Added complete push-plan validation before upstream push/editor/worktree/config/commit side effects; R2 later allowed bounded object hydration before validation. |
| R1 | Committed deletion behavior is undefined. | Valid. | Defined selected mirror path deletion as an unsupported sync push case with a clear diagnostic while preserving deletes inside existing mirror directories. |
| R1 | Preflight matrix omits write classification. | Valid. | Required `RequirementsFor(cli.CommandSync)` to include `MayWrite: true` and preflight coverage. |
| R2 | Push planning forbids the fetch it may need. | Valid. | Added bounded object hydration after scoped precheck and before push-plan validation, plus fresh-clone coverage and clear diagnostics for missing recorded revisions. |
| R3 | Upstream freshness is not explicitly part of full push-plan validation. | Valid. | Required freshness validation for every changed branch mirror before any push/editor action, with coverage proving a later stale branch prevents earlier pushes. |
| R4 | Remote-path-aware sync classification is underspecified. | Valid. | Required classification to use existing remote-path-aware item/tree semantics and added sync coverage for subdirectory and single-file mirrors. |
