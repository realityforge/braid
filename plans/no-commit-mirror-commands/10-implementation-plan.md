# No-Commit Mirror Commands Implementation Plan

Status: accepted

## Delivery Approach

Use the existing synthetic tree workflow as the behavioral anchor. Add CLI flags and option fields, then add a small command-layer helper that materializes selected paths from a synthetic final tree into the real index and worktree when `--no-commit` is set. Keep automatic-commit behavior intact except for the scoped pull config-hashing cleanup that uses `HashBytes` in the non-conflict success path.

The plan has been reviewed through iterative plan review and approved by the user's implementation request.

## Phase Sequence

1. CLI surface
   - Add `NoCommit` fields to add/update/remove options.
   - Parse `--no-commit` for `add`, `pull`/`update`/`up`, and `remove`.
   - Update usage and Bash completion. Root completion remains canonical-only, but command-specific option/path completion should work after a user types the `update` or `up` alias.
   - Add parser and completion unit tests.

2. Shared command support
   - Extend command Git interfaces only as needed for staging from a synthetic tree.
   - Add a helper that:
     - detects unrelated staged changes before staging;
     - warns when pre-existing unrelated staged changes exist;
     - restores selected paths from the final tree with staged and worktree updates;
     - prints a minimal success message unless quiet.

3. Add command behavior
   - Preserve current preflight and target availability checks.
   - Reuse existing final-tree creation.
   - If `--no-commit`, stage `.braids.json` and the mirror path instead of committing.
   - Preserve temporary remote cleanup and use staged-specific cleanup error wording.
   - Add unit tests for success, preservation, warnings, blockers, quiet, `HEAD` unchanged, first config creation, cleanup errors, and staging errors where practical.

4. Pull/update command behavior
   - Preserve no-op behavior.
   - Use `HashBytes` for config data in non-conflict success path.
   - If `--no-commit`, stage final tree paths instead of committing.
   - Support single mirror, aliases, no-path multi-mirror, deletions, config-only tracking changes, quiet, warnings, and dirty blockers.
   - For no-path `pull --no-commit`, preflight all eligible mirrors once before side effects, then process mirrors sequentially. Each successful mirror may stage its mirror path plus the currently updated `.braids.json` before the next mirror runs. Later same-invocation processing must not reject these already-staged Braid paths as dirty.
   - For no-path `pull --no-commit`, unrelated-staged warning detection must use the full set of eligible mirror paths selected by the invocation plus `.braids.json`, so earlier same-invocation staged mirrors are not reported as unrelated.
   - If a later no-path mirror errors or conflicts, leave prior same-invocation staged updates in place and report the error/conflict.
   - Keep current conflict behavior and output.

5. Remove command behavior
   - Reuse existing tree-without-mirror final tree.
   - If `--no-commit`, stage `.braids.json` and mirror deletion instead of committing.
   - Preserve `--keep` remote semantics.
   - Add unit tests for success, preservation, warnings, blockers, quiet, cleanup errors, staging errors, and `HEAD` unchanged.

6. Integration coverage
   - Add executable tests for multiple real paths, not just one happy path:
     - add no-commit with unrelated staged/unstaged/untracked work;
     - update no-commit with real upstream update and alias coverage;
     - no-path pull no-commit with multiple mirrors and locked skip;
     - remove no-commit with staged deletion and `--keep` remote behavior;
     - subdirectory invocation;
     - conflict path unchanged;
     - dirty owned-path blocker.

7. Documentation
   - Update README command sections and shared automatic-commit/scoped-work paragraphs.
   - Update migration notes if automatic commit behavior text needs the new exception.
   - Document warning/footgun for unrelated staged changes.

8. Validation and cleanup
   - Run targeted tests during implementation.
   - Run the full gate before claiming readiness.
   - Inspect final diff for accidental refactors, scratch output, and doc drift.

## High-Risk Areas And Mitigations

- Risk: staging helper overwrites unrelated user state.
  - Mitigation: only restore `.braids.json` and selected mirror pathspecs; add unit and integration preservation tests.
- Risk: unrelated staged warning counts Braid's own staged paths.
  - Mitigation: detect before staging; for no-path pull use the full invocation-selected mirror set as the command-owned exclusion; test warning and no-false-warning conditions.
- Risk: pull no-path partial progress differs from expected all-target safety.
  - Mitigation: make sequential partial staging explicit, keep all-target preflight before side effects, avoid same-invocation self-dirty checks, and test dirty eligible mirror blocks before updates plus later-conflict partial state.
- Risk: conflict path accidentally changes.
  - Mitigation: leave conflict branch mostly untouched; add no-commit conflict integration check.
- Risk: config file is dirtied on non-conflict failure.
  - Mitigation: use `HashBytes` for config JSON in success path where cleanly possible.
- Risk: docs imply `--no-commit` leaves unstaged changes.
  - Mitigation: docs consistently say staged without committing.

## Required Full Gate

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test --test_env=PATH //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
bazel test --test_env=PATH //integration/...
```

CI workflow note: `.github/workflows/ci.yml` also checks Git version and `git merge-tree --write-tree` before the Bazel test steps.

## Decision Outcomes

- `--no-commit` applies to `add`, `pull`, `update`, `up`, and `remove`; not `sync`.
- The flag stages Braid-owned paths only and leaves all unrelated state untouched.
- Dirty command-owned paths remain blockers.
- Success does not commit and does not write `MERGE_MSG`.
- Pull conflicts keep current behavior and output.
- Warnings are emitted for pre-existing unrelated staged files.
- Quiet suppresses success confirmations, not warnings.
- Unit tests cover all functionality; integration tests cover several executable paths.
- No-path `pull --no-commit` uses one upfront all-target preflight and intentionally permits same-invocation partial staging if a later mirror fails or conflicts.
- No-commit stdout/stderr behavior is fixed by the requirements output contract.

## Decision Log

| question | concrete plan change |
| --- | --- |
| Q-01 | Add parser/usage/completion support to canonical `pull`; aliases inherit it. |
| Q-02 | Shared staging helper stages only config plus selected mirror paths. |
| Q-03 | Existing scoped cleanliness checks remain in force. |
| Q-04 | Pull no-op path returns before staging. |
| Q-05 | Pull conflict branch remains current behavior. |
| Q-06 | No-path pull supports no-commit with existing all-target preflight. |
| Q-07 | Do not write non-conflict `MERGE_MSG`. |
| Q-08 | Add minimal success messages for staging. |
| Q-09 | No commit means no hook execution. |
| Q-10 | Preserve remote cleanup behavior. |
| Q-11 | First add can create and stage `.braids.json`. |
| Q-12 | `remove --keep` remains remote-only keep semantics. |
| Q-13 | Warn on unrelated staged changes. |
| Q-14 | Documentation says staged without committing. |
| Q-15 | Use `--no-commit`. |
| Q-16 | No sync support in this plan. |
| Q-17 | Do not reject unrelated staged changes. |
| Q-18 | Restore final tree to both index and worktree. |
| Q-19 | Base implementation on synthetic final trees. |
| Q-20 | Use synthetic tree for remove deletions. |
| Q-21 | Avoid real config writes in non-conflict success paths where possible. |
| Q-22 | Pull success path should use `HashBytes` if cleanly scoped. |
| Q-23 | Include CLI, completion, tests, and docs in one change. |
| Q-24 | Minimum test list expanded into unit and integration strategy. |
| Q-25 | Unit tests cover all behavior; integration tests cover multiple executable flows. |
| Q-26 | Preserve current ignored-file behavior. |
| Q-27 | Quiet suppresses success confirmation only. |
| Q-28 | Staged result includes deletions and renames as final tree expresses them. |
| Q-29 | Config-only tracking changes are staged. |
| Q-30 | Tests assert `HEAD` unchanged. |
| Q-31 | Cleanup failures after staging return errors and leave staged changes. |
| Q-32 | Cleanup failure wording uses "staged". |
| Q-33 | No broad rollback on staging failure. |
| Q-34 | No special support for repositories without initial commit. |
| Q-35 | Docs warn about already-staged unrelated files. |
| Q-36 | Add a small shared helper. |
| Q-37 | Warn before staging. |
| Q-38 | No special conflict output for no-commit. |
| Q-39 | Planning proceeds; implementation waits for plan review. |
| R1-01 | No-path `pull --no-commit` uses sequential partial staging after one upfront all-target preflight; implementation must avoid rejecting its own staged paths. |
| R1-02 | Targeted command plan uses existing Bazel targets, especially `bazel test //internal/command/...`, not nonexistent `//internal/command:command_test`. |
| R1-03 | Full gate uses CI-parity `bazel test --test_env=PATH //...` and `bazel test --test_env=PATH //integration/...`. |
| R1-04 | Add dedicated integration task `INT-001` with scenario acceptance criteria. |
| R1-05 | Completion remains root canonical-only but supports options/paths after typed `update` or `up` aliases. |
| R1-06 | Requirements specify exact no-commit stdout/stderr message and ordering contract. |
| R2-01 | No-path `pull --no-commit` warning detection excludes all mirror paths selected by the invocation, preventing false warnings from earlier same-invocation staged mirrors. |
