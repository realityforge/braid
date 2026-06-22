# CI Integration Platform Failures Requirements

Status: accepted
Date: 2026-06-22

## Mission

Fix the current cross-platform integration failures from GitHub Actions run
`27927786035` without weakening Braid's update, sync, push provenance, or
scoped-worktree safety guarantees.

The implementation must make update/sync behavior stable across the CI runner
Git versions and platforms, and must make the Windows integration editor helper
reliable enough to validate push provenance templates.

## Evidence From Existing Code And CI

- GitHub Actions run `27927786035` failed on push to `main` at
  `6bd080b02ab7381be14e752e273734094c7add92`.
- The `Go quality and lint` job passed.
- `Integration (linux-arm64)` passed with Git `2.54.0`.
- `Integration (darwin-arm64)` and `Integration (darwin-amd64)` failed in
  `bazel test //integration:braid_integration_test`.
- The macOS failures all entered Braid's update conflict path unexpectedly or
  missed Git's human `CONFLICT` text:
  - `TestExecutableUpdateConflictWritesMergeMessage`
  - `TestExecutableSubdirectoryConflictRecoveryCommands`
  - `TestExecutablePrimaryLifecycle`
  - `TestExecutableScopedUpdatePreservesUnrelatedState`
  - `TestExecutableSubdirectoryLifecycle`
  - `TestExecutableSyncPushesThenUpdates`
  - `TestExecutableSyncPullOnlyUpdatesWithoutEditor`
  - `TestExecutableSyncAutostashRestoresSelectedState`
- The Windows failure is isolated to
  `TestExecutablePushProvenanceTemplateTouchesGitDefaultTemplate`, where push
  stderr was `The system cannot find the file specified.\r\n`.
- `UpdateHandler.updateOne` currently builds base and remote synthetic trees,
  then calls `git merge-tree --write-tree --messages --merge-base=<baseTree>
  <localHead> <remoteTree>` for every non-noop update.
- `internal/gitexec.MergeTreeWrite` parses only the first stdout line as the
  merged tree and treats the remainder as unstructured details.
- `integration/support.go` creates Windows editor helpers as `.cmd` files that
  rely on `%~1` to locate Git's commit message file.
- Local uncached baseline on this machine passed:
  - `git --version`: `git version 2.51.0`
  - `bazel test //integration:braid_integration_test --test_output=errors --nocache_test_results`: pass

## Scope

In scope:

- Make clean update/sync cases bypass `git merge-tree` when the mirror item in
  `HEAD` is already equal to the recorded base item or the selected remote item.
- Treat an absent committed local mirror item as an explicit divergent state,
  not as equal to base or remote by default.
- Keep true divergent mirror updates on the existing synthetic merge path.
- Parse `merge-tree` conflict output using a structured conflict-file list
  rather than treating free-form messages as the behavioral contract.
- Define conflict parsing so message records cannot be mistaken for conflict
  path records.
- Preserve Braid's current conflict recovery state:
  - conflict markers are written to the mirror path when available,
  - `.braids.json` is updated and staged,
  - `.git/MERGE_MSG` is written,
  - unrelated staged paths remain staged,
  - subdirectory recovery commands remain runnable from the process directory.
- Make the Windows integration capture editor robust enough to copy Git's
  commit template and write the requested message.
- Add targeted tests for the new fast-path behavior, structured conflict
  parsing, and Windows editor helper behavior where locally testable.
- Run the repository CI parity gates before claiming implementation readiness.

Out of scope:

- Do not change the public `braid sync` command surface.
- Do not change push provenance content or selection semantics.
- Do not add compatibility shims for unsupported Git versions below the
  repository's existing minimum Git version.
- Do not weaken dirty `.braids.json`, unresolved operation, or scoped path
  cleanliness checks.
- Do not replace Braid's temporary-index update commit strategy with a normal
  index commit.

## Locked Decisions And Non-Negotiables

- Q-01 resolved: Braid owns a deterministic conflict summary line derived from
  structured conflict paths, so executable behavior does not depend on Git's
  free-form `merge-tree` messages containing `CONFLICT`.
- Product code that invokes Git remains behind `internal/gitexec`.
- Integration tests must not depend on the user's global Git identity, real
  Braid cache, or network remotes.
- Tests must continue configuring local user identity and disabling GPG signing.
- Existing successful Linux behavior is the baseline; fixes must not special
  case CI runner names.
- The final implementation must pass `.github/workflows/ci.yml` parity before
  being reported ready.

## Command Surface And Behavior Expectations

- `braid update <local_path>` remains the user-facing command for updating one
  mirror.
- `braid sync [local_path...]` remains the user-facing command for push-then-
  update branch mirror workflows.
- Clean update behavior:
  - If `HEAD:<mirror>` equals the recorded base item, update directly to the
    remote item and commit mirror plus config changes.
  - If `HEAD:<mirror>` equals the selected remote item, commit only the config
    revision update.
  - These paths must preserve unrelated staged, unstaged, and untracked state.
- Divergent update behavior:
  - If local mirror content differs from both base and selected remote content,
    use the synthetic merge path.
  - If the committed local mirror path is absent from `HEAD`, classify it as
    divergent and keep existing merge/delete behavior unless implementation
    evidence proves a narrower deliberate failure contract is required.
  - If the synthetic merge conflicts, preserve existing recovery instructions
    and conflict-state side effects.
- Structured conflict parsing:
  - Use an explicit `merge-tree` invocation and parser contract that separates
    the merged tree OID, conflicted-file section, and informational message
    section.
  - When using NUL-delimited output, read conflict paths after the tree OID
    until the first empty NUL record; treat everything after that sentinel as
    informational message records.
  - Do not infer that a merge is clean from an empty conflicted-file section;
    conflict status comes from the Git exit status.
  - Deduplicate conflict paths while preserving deterministic output order.
  - If Git reports a conflict but no structured path is available, print a
    deterministic fallback conflict summary that still contains `CONFLICT` and
    preserves the existing recovery instructions.
- Push provenance integration behavior:
  - `TestExecutablePushProvenanceTemplateTouchesGitDefaultTemplate` must verify
    that Git's default commit template includes Braid provenance guidance before
    the editor writes the final commit message.
  - The Windows editor helper must not rely on ambiguous shell argument handling
    that loses Git's commit message file path.

## Quality Gates

Required CI parity gates from `.github/workflows/ci.yml`:

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
bazel test //integration:braid_integration_test
```

Targeted gates during implementation:

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

Cross-platform validation:

- Local checks can prove behavior on the current macOS host.
- A GitHub Actions rerun or successor run is required to prove that the named
  target failures are fixed on Windows Server 2025, macOS 15 Intel, and macOS
  15 Arm. Ubuntu arm64 and the Go quality job must remain passing.

## Known Intentional Divergences

- The local baseline does not reproduce the CI macOS failures with Git `2.51.0`;
  the plan treats this as a platform/Git-version compatibility failure.
- The Windows provenance failure appears to be test harness reliability, not a
  requested product behavior change.

## Approval

- Plan reviewed through three iterative plan-review rounds.
- User approved implementation by requesting "implement in subagent" on
  2026-06-22.
- Approval sequencing evidence is the conversation event above, not commit
  chronology: the implementation commit was created before the plan-document
  commit, after the accepted plan had existed in the working tree.

## Open Questions Register

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | Should Braid print its own deterministic conflict summary line, or should tests stop asserting that stdout contains `CONFLICT`? | Current integration tests assert `CONFLICT` because Git previously printed it in `merge-tree --messages` output. The failing macOS logs show Braid wrote conflict instructions without Git's `CONFLICT` text. Git documents merge-tree informational messages as human-oriented and unstable. | A: Add a Braid-owned `CONFLICT: <path>` line based on structured conflict paths. B: Remove the test assertion and rely on `Braid: conflicts written...` plus conflict markers. C: Continue relying on Git free-form messages when present. | A changes stdout slightly but gives users and tests a stable conflict cue. B avoids a public output change but weakens executable coverage of conflict reporting. C keeps current behavior but leaves CI exposed to platform/Git message drift. | A: add a Braid-owned deterministic conflict summary line before Braid recovery instructions. | A: add a Braid-owned deterministic conflict summary line before Braid recovery instructions. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
