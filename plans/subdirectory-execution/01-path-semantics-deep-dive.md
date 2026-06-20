# Path Semantics Deep Dive

## Feature

- Name: Subdirectory execution path semantics
- Owner: Braid CLI
- Related plan/task IDs: `T01`, `T02`, `T04`, `T05`

## Problem Statement

Braid currently assumes the process directory is the downstream Git worktree root. Supporting subdirectory execution requires a single path model that handles user inputs, config paths, Git pathspecs, Git metadata paths, cache paths, diff passthrough, temporary indexes, and conflict recovery without changing the persisted config contract.

## Scope

In scope:

- CLI `local_path` normalization.
- Repo-root config lookup.
- Internal repo-root path representation.
- Git command workdir selection.
- Diff passthrough behavior.
- Conflict recovery command paths.

Out of scope:

- Changing `.braids.json` schema.
- Storing absolute paths.
- Selecting containing mirrors from nested paths.
- Parsing arbitrary Git diff passthrough arguments.

## Inputs And Interfaces

- `local_path` positionals for `add`, `setup`, `status`, `diff`, `update`, `push`, and `remove`.
- Optional omitted `local_path` for `add`.
- Global `--cache-dir`.
- Environment `BRAID_LOCAL_CACHE_DIR`.
- Raw `diff` passthrough args after `--`.

## Behavior Requirements

1. Resolve the process directory and Git worktree root at command runtime.
2. Convert user `local_path` inputs to repo-root-relative slash paths before config lookup/storage.
3. Treat relative `local_path` inputs as process-directory-relative.
4. Treat omitted `add` default basenames as process-directory-relative.
5. Accept absolute `local_path` inputs only when they logically resolve inside the worktree.
6. Reject normalized paths that escape the worktree.
7. Keep normalized mirror paths compatible with existing `pathcheck.ValidateLocal`.
8. Keep no-path commands repository-wide.
9. Preserve raw Git behavior for `diff` passthrough args.
10. Keep ordinary output and commit subjects repo-root-relative.
11. Print conflict recovery snippets that work from the invocation directory.

## RepoContext Contract

The implementation must create and pass a command runtime context with:

- `ProcessWorkDir`: invocation directory for cache paths and final raw-passthrough `git diff`.
- `GitWorkTreeRoot`: `git rev-parse --show-toplevel` result for `.braids.json` and Braid-owned Git commands.
- `WorkTreePrefix`: authoritative slash prefix from worktree root to process directory; relative CLI mirror paths are joined to this prefix.
- `LogicalWorkTreeRoot`: optional root derived from `ProcessWorkDir` only when its lexical suffix matches `WorkTreePrefix`; it exists solely to accept absolute inputs using the invocation's logical spelling.
- root and process Git runners so handlers do not accidentally use one workdir for both root-owned and process-owned Git semantics.

`Options.WorkDir` means invocation directory. `Options.ConfigRoot` is a test override only. Production code must not use `Options.WorkDir` as a config root after context resolution.

## Normalization Algorithm

- Relative input: normalize separators, join to `WorkTreePrefix`, slash-clean, and reject escape above the root.
- Omitted `add` input: derive the basename, then treat it as relative input.
- Absolute input: accept if it is lexically under available `LogicalWorkTreeRoot` or under `GitWorkTreeRoot`, then convert to a repo-root-relative slash path.
- Internal symlink cwd mismatch: if `ProcessWorkDir` does not lexically end with `WorkTreePrefix`, `LogicalWorkTreeRoot` is unavailable. Relative inputs still use Git's prefix; absolute symlink-spelled inputs are rejected unless they also fall under `GitWorkTreeRoot`.
- Symlinks: do not realpath mirror inputs for containment. Logical Git paths are the user-facing contract.
- Selectors: after normalization, require an exact config key match.

## Diff Contract

- The final user-visible `git diff` runs from `ProcessWorkDir`.
- Directory mirrors use `--relative=<repo-root-mirror-path>/` and raw passthrough args unchanged.
- Single-file mirrors use the existing source/destination prefixes and append the internal pathspec as `:(top)<repo-root-mirror-path>`.
- No-path diff loops over every configured mirror and applies the same rules per mirror.

## Conflict Command Contract

Conflict output must print shell-copyable commands using this shape:

```bash
git add -- ':(top)<repo-root-mirror-path>' ':(top).braids.json'
git commit -F '<process-dir-relative-or-absolute MERGE_MSG path>'
```

The `MERGE_MSG` operand must be resolved from `ProcessWorkDir` using `git rev-parse --git-path MERGE_MSG`.

## Error Handling And Diagnostics

- Outside-worktree execution fails during preflight.
- Escaping paths fail before config lookup or mutation.
- Invalid normalized mirror paths use existing pathcheck errors.
- Exact selector misses return `mirror does not exist: <repo-root-relative-path>`.
- Conflict snippets should avoid paths that only work from the repository root.

## Compatibility And Parity

Baseline behavior:

- Root execution remains valid.
- Existing root-relative command examples continue to work when invoked from the root.
- `.braids.json` remains repo-root-relative and portable.
- Relative cache paths remain process-directory-relative, preserving existing tests.

Intentional divergences:

- The same relative `local_path` spelling can now resolve differently depending on process directory.
- Absolute local path inputs inside the worktree become valid CLI input.
- Root-only preflight rejection is removed.

## Non-Functional Requirements

- Deterministic path normalization across Unix and Windows path separators.
- No broad process `chdir`.
- No hidden compatibility shim for root-only behavior.
- No ad hoc parsing of Git's full diff option/pathspec grammar.

## Acceptance Criteria

- [ ] `braid status <relative-path>` from a subdir finds the configured mirror after cwd-relative normalization.
- [ ] `braid add <url> vendor/foo` from `apps/web` stores `apps/web/vendor/foo`.
- [ ] `braid add <url>` from `apps/web` stores `apps/web/<derived-basename>`.
- [ ] `braid update ../../vendor/foo` from `apps/web` resolves to `vendor/foo`.
- [ ] Absolute input inside the worktree resolves to a repo-root-relative path.
- [ ] Absolute input through a symlink-spelled worktree path resolves logically when inside the worktree.
- [ ] Invocation from an internal symlink whose target has a different Git prefix uses `WorkTreePrefix` for relative inputs and rejects unsupported absolute symlink-spelled inputs with a clear diagnostic.
- [ ] Absolute input outside the worktree is rejected.
- [ ] `.` at a mirror root selects that mirror; `.` inside a mirror subdirectory does not select the containing mirror.
- [ ] No-path `status`, `diff`, `setup`, and `update` remain repository-wide.
- [ ] `diff` passthrough pathspecs behave like raw Git from the invocation directory.
- [ ] Conflict output gives commands that can be copied and executed from the invocation directory.

## Validation Plan

Targeted checks:

```bash
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

Full gate:

```bash
bazel test //...
```

## Open Questions

None.
