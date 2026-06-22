# CI Integration Platform Failures Implementation Plan

Status: accepted
Date: 2026-06-22

## Phase Sequence

1. Resolve open planning questions
   - `Q-01` is resolved.
2. Plan review and approval
   - Complete: iterative plan review ran for three rounds.
   - Complete: user approved implementation by requesting subagent execution.
3. Update fast-path classification
   - Add update planning logic that compares the current mirror item in `HEAD`
     with the recorded base item and selected remote item.
   - Skip `git merge-tree` when the local mirror is unchanged from base or
     already equal to remote.
   - Represent the current mirror item with an explicit state:
     `present`, `absent`, or `error`.
   - Treat `absent` as divergent so committed mirror deletions continue through
     the synthetic merge/delete path unless implementation evidence requires a
     deliberate failure contract.
   - Commit the same final tree shape as the existing update path would have
     produced.
4. Structured conflict output
   - Extend `internal/gitexec.MergeTreeWrite` to expose structured conflict
     paths in addition to the merged tree and human details.
   - Use `git merge-tree --write-tree --name-only -z --messages
     --merge-base=<baseTree> <localTreeish> <remoteTreeish>` or an equivalent
     invocation with the same parse boundaries.
   - Parse the output as:
     - merged tree OID terminated by NUL,
     - zero or more conflicted path records terminated by NUL,
     - an empty NUL record that terminates the conflicted-path section,
     - informational message records after the conflict path section.
   - After reading the tree OID, read conflicted paths only until the first
     empty NUL record; everything after that sentinel belongs to informational
     message records and must not be parsed as conflict paths.
   - Treat Git exit status, not an empty conflicted-path list, as the clean vs
     conflict signal.
   - Deduplicate structured conflict paths while preserving deterministic output
     order.
   - If Git reports conflict status without a structured path, expose a
     fallback conflict marker so Braid can still print deterministic conflict
     output.
   - Preserve existing human details when Git provides them.
   - Print a Braid-owned deterministic `CONFLICT: <path>` summary line for each
     structured conflict path before Braid recovery instructions.
5. Windows integration editor robustness
   - Replace or harden the Windows capture editor helper so Git's commit message
     file path is reliably copied before the final message is written.
   - Keep non-Windows helper behavior unchanged unless shared cleanup is needed.
6. Targeted coverage and full validation
   - Add unit/command tests for update fast paths and conflict parsing.
   - Add or adjust executable integration tests for the macOS failure cases and
     Windows provenance capture.
   - Run targeted gates while iterating.
   - Run the full CI parity gates before reporting code ready.
   - Rerun GitHub Actions or otherwise inspect the next CI run for Windows and
     both macOS runners.

## Delivery Approach

- Keep behavior changes small and tied directly to the failing paths.
- Prefer explicit update classification over defensive retries around
  `merge-tree`.
- Keep all Git invocation changes inside `internal/gitexec`.
- Avoid platform-specific product code branches unless a real Git-for-platform
  behavior difference remains after structured parsing and fast paths.
- Treat the Windows provenance issue as a test harness fix unless further
  evidence shows product code is passing an invalid editor/template path.

## High-Risk Areas

- Fast-path tree equivalence
  - Impact: incorrect equality checks could skip a real merge or produce an
    incorrect update commit.
  - Mitigation: compare Git tree/blob item type, mode, and hash; cover tree
    mirrors, remote-path mirrors, subdirectory invocation, and config-only
    revision update cases.
- Committed mirror deletion classification
  - Impact: treating an absent `HEAD:<mirror>` path as an ordinary lookup error
    could block updates that currently reach merge/delete handling.
  - Mitigation: represent absent local mirror items explicitly and test that
    committed local deletions remain divergent rather than fast-pathed.
- Config-only update commit
  - Impact: `sync` after push expects a downstream Braid update commit even
    when mirror content already matches the remote.
  - Mitigation: add command and integration coverage for local mirror equals
    remote but config revision is stale.
- Conflict path parsing
  - Impact: `merge-tree` output has multiple sections and can include paths
    with spaces or quoting.
  - Mitigation: use `--name-only -z` or equivalent structured parse boundaries;
    test the empty NUL section terminator, paths with spaces, message records,
    empty conflict-path lists with conflict exit status, and conflict output
    that lacks free-form `CONFLICT`.
- Existing conflict recovery
  - Impact: update conflict state is user-visible and used by sync autostash.
  - Mitigation: preserve status values, stdout/stderr split, staged config,
    `MERGE_MSG`, and recovery command tests.
- Windows editor helper
  - Impact: changes may only be fully observable on Windows CI.
  - Mitigation: make the helper validate arguments and fail with a diagnostic;
    keep the script simple and rerun Windows integration in CI.

## Required Full Gate

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
bazel test //integration:braid_integration_test
```

## Targeted Gates

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
bazel test //integration:conflict_test
bazel test //integration:lifecycle_test
bazel test //integration:subdirectory_test
bazel test //integration:sync_test
bazel test //integration:scoped_state_test
bazel test //integration:braid_integration_test --test_output=errors --nocache_test_results
```

## Decision Outcomes

- `Q-01` is resolved: Braid prints a deterministic conflict summary line derived
  from structured conflict paths.
- Round 1 plan review resolved:
  - conflict parser contract now specifies parse boundaries and fallback
    behavior,
  - fast-path tests must prove `MergeTreeWrite` is bypassed,
  - absent committed mirror items are classified as divergent,
  - completion requires target CI failures to pass.
- Round 2 plan review resolved:
  - conflict parser contract now names the empty NUL record that terminates the
    conflicted-path section.
- No behavior questions remain open.

## Decision Log

| id | Plan change |
| --- | --- |
| Q-01 | Add structured conflict-path parsing and print a Braid-owned `CONFLICT: <path>` line before recovery instructions instead of depending on Git free-form `merge-tree --messages` text. |

## Implementation Tasks

Tasks are tracked in `20-task-board.yaml`.

- `PLAN-CONTEXT`: collect evidence and draft this plan.
- `PLAN-QUESTIONS`: resolve open questions.
- `PLAN-APPROVAL`: review and accept or revise the plan.
- `T01`: implement update fast-path classification.
- `T02`: implement structured merge-tree conflict parsing and output behavior.
- `T03`: harden Windows integration editor capture.
- `T04`: run full local gates and verify remote CI.

## Completion Criteria

- All open questions are resolved and recorded.
- User has reviewed and accepted the plan.
- Every planned task in `20-task-board.yaml` is completed.
- macOS update/sync integration tests no longer enter conflict path for clean
  update cases.
- Command-level tests prove fast-path cases do not invoke `MergeTreeWrite`.
- True update conflict tests still produce conflict markers, recovery
  instructions, and `MERGE_MSG`.
- Windows push provenance integration test can capture the default Git commit
  template without editor helper path errors.
- All local CI parity gates pass.
- Follow-up GitHub Actions run shows the named target failures fixed on both
  macOS integration jobs and the Windows integration job. Residual failures may
  be documented only if they are unrelated to this plan's target failures.
