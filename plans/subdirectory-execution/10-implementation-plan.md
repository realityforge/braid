# Subdirectory Execution Implementation Plan

Status: accepted
Date: 2026-06-20

## Phase Sequence

1. Planning approval
   - Review `00-requirements.md`, `01-path-semantics-deep-dive.md`, this plan, `20-task-board.yaml`, and `40-test-strategy.md`.
   - Apply review feedback before implementation.
2. Repository context
   - Add Git wrapper support for `rev-parse --show-toplevel`.
   - Resolve a `RepoContext` containing process directory, Git worktree root, logical worktree root, process prefix, root Git runner, and process Git runner.
   - Replace root-only preflight with inside-worktree validation plus context setup.
   - Return/pass the context to handlers so config loading, path normalization, root Git work, and process-dir diff work do not rediscover or disagree about paths.
   - Keep test hooks usable through `Options`: `Options.WorkDir` is the invocation directory, `Options.ConfigRoot` is a test-only config override, and injected Git remains supported for unit tests.
3. Path normalization
   - Add one command-level normalization path for CLI `local_path` inputs.
   - Normalize relative, absolute, dot, parent, and backslash inputs into repo-root-relative slash paths.
   - Reject paths that escape the worktree.
   - Apply normalization to every command selector.
   - Apply process-directory-relative default path placement for omitted `add` local paths.
4. Root-owned Git/config execution
   - Load and write `.braids.json` from the worktree root.
   - Run Braid-owned Git operations from the worktree root where pathspecs are repo-root-relative.
   - Ensure temporary-index, restore, config, remote, push alternates, and metadata helpers use the correct root or Git-path source.
5. Diff and conflict command behavior
   - Run the final user-visible `git diff` from the process Git runner.
   - Preserve raw `git diff` passthrough behavior from the invocation directory.
   - For directory mirrors, use `--relative=<repo-root-mirror-path>/` and do not add plain repo-root pathspecs that would be reinterpreted under the process directory.
   - For single-file mirrors, append `:(top)<repo-root-mirror-path>` as Braid's internal limiter.
   - Update conflict instructions to print shell-copyable commands runnable from the process directory:
     - `git add -- ':(top)<repo-root-mirror-path>' ':(top).braids.json'`
     - `git commit -F '<process-dir-relative-or-absolute MERGE_MSG path>'`
   - Resolve the displayed `MERGE_MSG` path with process Git, not root Git.
6. Integration and docs
   - Add integration coverage for a representative subdirectory workflow.
   - Cover path normalization edge cases and exact selector behavior.
   - Update README usage, command form, and paths section.
7. Validation
   - Run targeted tests while implementing.
   - Run full `bazel test //...` before completion.
   - Record evidence in `20-task-board.yaml`.

## Delivery Approach

- Keep the internal persisted path format unchanged: repo-root-relative slash paths.
- Centralize runtime context and normalization instead of adding per-command ad hoc conversions.
- Thread `RepoContext` through handlers; do not let handlers call `configRoot(options)` or `workDir(options.WorkDir)` as implicit root substitutes after preflight.
- Preserve package boundaries:
  - Git command wrappers in `internal/gitexec`.
  - Runtime context and command path normalization in `internal/command`.
  - Config serialization in `internal/config` unchanged unless a test proves otherwise.
  - CLI parsing in `internal/cli` remains syntax-only where practical.
- Use existing `pathcheck` validation after normalization rather than weakening stored path rules.
- Avoid global process `chdir`.

## High-Risk Areas

- Git pathspec interpretation
  - Impact: commands could inspect or mutate `subdir/vendor/foo` instead of `vendor/foo`.
  - Mitigation: run Braid-owned Git operations from worktree root; add subdirectory integration coverage.
- Config root resolution
  - Impact: commands from subdirs could create or read the wrong `.braids.json`.
  - Mitigation: context-derived config root; tests for subdir status/add/update.
- Diff passthrough
  - Impact: rewriting raw args could break valid Git invocations.
  - Mitigation: run final diff from `ProcessWorkDir`, preserve raw passthrough behavior, and only anchor Braid-owned paths.
- Conflict recovery instructions
  - Impact: printed commands could fail when copied from a subdir.
  - Mitigation: specify exact command shape and run it from the original subdirectory in integration coverage.
- Absolute and escaping paths
  - Impact: outside-worktree paths could become invalid config entries or misleading selectors.
  - Mitigation: use `WorkTreePrefix` as the only source of truth for relative paths; use lexical containment against available `LogicalWorkTreeRoot` and `GitWorkTreeRoot` for absolute paths; reject internal symlink absolute spellings that cannot be mapped safely; add symlink-spelled worktree and internal symlink cwd coverage.
- Linked worktrees
  - Impact: hard-coded `.git` assumptions can write metadata into the wrong directory.
  - Mitigation: continue using `git rev-parse --git-path` for metadata and object paths.

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

## Decision Outcomes

- Q-01 through Q-11 are resolved in `00-requirements.md`.
- No behavior question remains open.
- Plan was accepted by the user on 2026-06-20 after iterative plan review returned `Findings: none`.

## Decision Log

| id | Plan change |
| --- | --- |
| Q-01 | Normalize CLI `local_path` values relative to process directory, then use repo-root-relative paths internally. |
| Q-02 | Preserve raw `diff` passthrough behavior; anchor only Braid-owned internal diff paths. |
| Q-03 | Place omitted `add` derived paths under the process directory. |
| Q-04 | Generate conflict recovery snippets that work from the invocation directory. |
| Q-05 | Keep relative cache paths process-directory-relative. |
| Q-06 | Resolve central repository context and use worktree root for Braid-owned Git/config work. |
| Q-07 | Preserve repository-wide no-path command semantics. |
| Q-08 | Accept absolute local path inputs only when logically inside the worktree. |
| Q-09 | Use logical Git path normalization, not symlink realpath normalization. |
| Q-10 | Keep ordinary path output repo-root-relative. |
| Q-11 | Require exact configured mirror-root selector matches after normalization. |

## Plan Review Finding Log

| round | finding | assessment | plan change |
| --- | --- | --- | --- |
| R1 | Missing runtime context plumbing. | Valid. | Added explicit `RepoContext` contract, handler propagation requirement, and `Options` semantics. |
| R1 | Diff passthrough workdir is underspecified. | Valid. | Specified process-run final diff, directory mirror rules, single-file `:(top)` limiter, and no-path diff behavior. |
| R1 | Logical symlink containment lacks an algorithm. | Valid. | Added logical/root normalization algorithm using `WorkTreePrefix`, `LogicalWorkTreeRoot`, and `GitWorkTreeRoot`. |
| R1 | Push subdirectory coverage is missing. | Valid. | Added push to lifecycle/integration coverage and task acceptance. |
| R1 | Conflict recovery acceptance is too weak. | Valid. | Specified exact printed command shape and required executing it from the original subdirectory. |
| R2 | Symlinked invocation-directory normalization still has an unresolved case. | Valid. | Made `WorkTreePrefix` authoritative for relative paths, made `LogicalWorkTreeRoot` optional, and required tests/diagnostics for internal symlink cwd mismatch. |

## Completion Criteria

- User has reviewed and accepted the plan.
- All implementation tasks in `20-task-board.yaml` are completed.
- README reflects final behavior.
- Unit and integration tests cover agreed semantics.
- `bazel test //...` passes.
- Task evidence is recorded.
- Working tree is clean or any exception is explicitly documented.
