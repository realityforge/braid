# Subdirectory Execution Requirements

Status: accepted
Date: 2026-06-20

## Mission

Allow Braid repository commands to run from any subdirectory inside the downstream Git worktree while preserving Braid's existing repo-root-relative config, commit history, and ordinary output semantics.

## Evidence From Current Code

- `Preflight` currently rejects any non-empty `git rev-parse --show-prefix` result with `braid v1 must run from the git working tree root`.
- `.braids.json` is loaded from `configRoot(options)`, which defaults to `Options.WorkDir`; from a subdirectory this would look in the wrong place.
- Config keys and `mirror.Mirror.Path` values are repo-root-relative slash paths.
- CLI parsing currently normalizes `\` to `/` but does not resolve paths against the process directory or repo root.
- Git operations in `internal/gitexec` execute from `Runner.WorkDir`; pathspec-sensitive commands currently assume that workdir is the repo root.
- `braid diff` intentionally stores args after `--` as raw `GitDiffArgs`.
- Conflict instructions currently print root-style paths and `.git/MERGE_MSG`, which are not generally runnable from a subdirectory.
- Local Git probes during planning showed:
  - From `repo/sub`, `git status -- vendor/repo` targets `sub/vendor/repo`.
  - `:(top)vendor/repo` reaches the repo-root path from a subdirectory.
  - Running Braid-owned Git operations from the worktree root avoids most pathspec ambiguity.
  - `git rev-parse --git-path MERGE_MSG` is the correct source for worktree-specific metadata paths, including linked worktrees.
  - For single-file mirror diffs from a subdirectory, the internal file pathspec must be top-anchored; a plain repo-root pathspec produces no diff.

## Scope Boundaries

In scope:

- All repository commands: `add`, `setup`, `status`, `diff`, `update`, `push`, and `remove`.
- Running commands from any directory inside the Git worktree.
- Resolving `.braids.json` from the Git worktree root.
- Normalizing CLI `local_path` inputs relative to the process directory before internal config lookup/storage.
- Supporting absolute `local_path` inputs only when they logically resolve inside the worktree.
- Keeping no-path command behavior repository-wide.
- Preserving raw `git diff` passthrough behavior after `--`.
- Making conflict recovery command snippets runnable from the original process directory.
- Updating README and tests.

Out of scope:

- Running Braid outside the downstream Git worktree.
- Automatically selecting a containing mirror when a selector points inside a mirror subdirectory.
- Changing `.braids.json` to store process-directory-relative or absolute paths.
- Adding fallback code for legacy root-only behavior.
- Scoping no-path commands to the current directory.
- Parsing and rewriting arbitrary `git diff` passthrough arguments.

## Locked Decisions And Non-Negotiables

- CLI `local_path` values are interpreted relative to the process directory, then normalized to repo-root-relative slash paths internally.
- Omitted `braid add <url>` paths are derived under the process directory.
- Relative `--cache-dir` and relative `BRAID_LOCAL_CACHE_DIR` remain process-directory-relative.
- Braid resolves a central repository context once: process directory, worktree root, and process-dir prefix.
- Braid-owned Git/config work uses the worktree root unless the behavior explicitly requires the process directory.
- No-path commands remain repository-wide from any subdirectory.
- Absolute `local_path` inputs are accepted only when they logically resolve inside the worktree; stored paths remain repo-root-relative.
- Mirror path normalization uses logical Git-path semantics, not physical symlink realpath semantics.
- Ordinary output remains repo-root-relative.
- Copy-paste recovery commands printed after conflicts must be runnable from the original process directory.
- Selectors must resolve exactly to configured mirror roots after normalization.

## Repository Runtime Context Contract

Repository commands must resolve a `RepoContext` once and pass it through command handlers instead of rediscovering root/config state independently.

Required context fields:

- `ProcessWorkDir`: the invocation directory. It is the base for relative cache paths and final `git diff` calls that include raw passthrough args.
- `GitWorkTreeRoot`: the worktree root reported by `git rev-parse --show-toplevel`. It is the base for config load/write and Braid-owned Git operations.
- `WorkTreePrefix`: the slash-form prefix from `git rev-parse --show-prefix`, with no trailing slash in stored context. It is the authoritative base for relative CLI `local_path` inputs.
- `LogicalWorkTreeRoot`: an optional logical root derived by removing `WorkTreePrefix` path elements from `ProcessWorkDir` only when that suffix relationship is lexically true. It is used only for accepting absolute inputs that use the invocation's logical spelling.
- `RootGit`: a Git runner whose `WorkDir` is `GitWorkTreeRoot`.
- `ProcessGit`: a Git runner whose `WorkDir` is `ProcessWorkDir`, used only where process-directory Git semantics are required.

`Options.WorkDir` remains the invocation directory hook for tests and non-default callers. `Options.ConfigRoot` remains a test-only override for config filesystem location; production context uses `GitWorkTreeRoot`. Injected `Options.Git` must still be usable by unit tests, but production code should create root/process Git runners from the resolved context rather than assuming one runner can satisfy both workdir semantics.

## Command Surface And Behavior Expectations

`braid add <url> [local_path]`:

- When `local_path` is present, resolve it relative to the process directory unless it is absolute.
- When `local_path` is omitted, derive the basename from the upstream URL or `--path` and place it under the process directory.
- Store the mirror path in `.braids.json` as a repo-root-relative slash path.
- Keep existing validation, cleanliness, commit, remote, and cache behavior.

`braid setup [local_path]`, `braid status [local_path]`, `braid diff [local_path]`, `braid update [local_path]`, `braid push <local_path>`, `braid remove <local_path>`:

- Resolve any `local_path` relative to the process directory and require it to match a configured mirror root exactly after normalization.
- Keep no-path forms repository-wide where they exist today.
- Keep user-facing mirror path output repo-root-relative.

`braid diff [local_path] -- <git_diff_arg>...`:

- Preserve passthrough args exactly as raw Git arguments.
- Do not parse arbitrary passthrough pathspecs.
- Anchor only Braid-owned internal paths where needed.
- Preserve existing mirror-relative diff output.
- Run the final user-visible `git diff` from `ProcessWorkDir`.
- For directory/tree mirrors, keep Braid's mirror-relative output with `--relative=<repo-root-mirror-path>/` and do not add an internal pathspec unless one is needed for a narrower Braid-owned operation.
- For single-file mirrors, append the internal file limiter as `:(top)<repo-root-mirror-path>` so the final diff works from subdirectories.
- For no-path `braid diff`, process every configured mirror using the same per-mirror rules; the command remains repository-wide.

Conflict handling:

- Write Git metadata paths using `git rev-parse --git-path`, converted relative to the correct process/repo context.
- Print recovery command snippets that work from the invocation directory.
- Print this command shape, with dynamic path arguments shell-quoted for spaces and metacharacters:
  - `git add -- ':(top)<repo-root-mirror-path>' ':(top).braids.json'`
  - `git commit -F '<process-dir-relative-or-absolute MERGE_MSG path>'`
- Resolve the displayed `MERGE_MSG` operand with `git rev-parse --git-path MERGE_MSG` from `ProcessWorkDir`, not from `GitWorkTreeRoot`.

## Diagnostics

- Outside-worktree commands continue to fail with a clear "inside a git working tree" error.
- Paths that resolve outside the worktree fail before config lookup or mutation.
- Absolute paths inside the worktree are reported/stored as repo-root-relative paths after normalization.
- Exact selector misses continue to report `mirror does not exist: <normalized-path>`.
- Invalid stored mirror paths continue to use existing `pathcheck` diagnostics.

## Path Normalization Algorithm

- Normalize `\` to `/` for CLI `local_path` inputs before slash-path processing.
- For relative inputs, compute `path.Clean(path.Join(WorkTreePrefix, input))`; reject results that are `.`, empty for selector commands, or start with `../`. Do not derive relative mirror paths from the `ProcessWorkDir` filesystem spelling.
- For omitted `add` local paths, first derive the basename, then apply the same relative-input rule.
- For absolute inputs, first try lexical containment under `LogicalWorkTreeRoot` when it is available; if it matches, convert the filesystem-relative suffix from `LogicalWorkTreeRoot` to a slash path.
- If the logical-root check fails, try lexical containment under `GitWorkTreeRoot`; if it matches, convert the filesystem relative path to a slash path.
- Reject absolute inputs that are under neither root.
- If `ProcessWorkDir` is an internal symlink path whose lexical suffix does not match `WorkTreePrefix`, relative inputs are still supported through `WorkTreePrefix`, but absolute inputs using that symlink spelling are rejected unless they are also under `GitWorkTreeRoot`.
- Do not call `EvalSymlinks` as part of mirror path containment. Existing filesystem checks and Git commands remain responsible for rejecting symlink ancestors that cannot be materialized as mirror directories.

## Quality And Coverage Gates

Required full gate:

```bash
bazel test //...
```

Targeted gates:

```bash
bazel test //internal/command:command_test
bazel test //internal/gitexec:gitexec_test
bazel test //integration:braid_integration_test
```

Coverage requirements:

- Unit tests for repository context resolution and root config lookup.
- Unit tests for relative, absolute, dot, parent traversal, backslash, outside-worktree, and symlink-logical path normalization.
- Unit tests for invocation from an internal symlink whose target has a different Git prefix; relative inputs follow `WorkTreePrefix`, while unsupported absolute symlink-spelled inputs fail clearly.
- Unit tests proving no-path commands stay repository-wide.
- Unit tests or integration tests proving root-owned Git operations run correctly from subdirectories.
- Tests for `diff` passthrough preserving raw Git behavior from subdirectories.
- Tests for conflict instruction snippets from subdirectories, including execution of the printed command shape.
- Integration coverage for at least one full add/setup/status/diff/update/push/remove workflow from a subdirectory.
- README updated to remove the root-only command limitation and document new path semantics.

## Intentional Divergences

- Braid no longer intentionally requires repository commands to start from the worktree root.
- Explicit `local_path` arguments from subdirectories can now map to different repo-root paths than the same spelling at the root.
- Absolute local path inputs are accepted as CLI input when inside the worktree, while stored config remains relative.

## Open Questions Register

All questions are resolved from the grill-me session. The plan remains pending user review and must not be marked accepted until review feedback is applied.

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | How should `local_path` arguments be interpreted from a subdirectory? | Config stores repo-root-relative paths, but users invoking from subdirs expect cwd-relative inputs. | Process-dir-relative, repo-root-relative, or accept both. | Process-dir-relative matches normal CLI expectations; repo-root-relative is simpler but surprising; accepting both is ambiguous. | Process-dir-relative input, repo-root-relative internally. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-02 | resolved | How should `braid diff ... -- <git_diff_arg>...` path-like passthrough args be interpreted? | Args after `--` are raw Git args today. | Preserve Git cwd behavior, make root-relative, or parse/rewrite. | Preserving raw args avoids partial Git parser risk. | Preserve raw process-dir Git behavior; anchor only Braid-owned paths. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-03 | resolved | Where should omitted `braid add <url>` default paths land from a subdirectory? | `mirror.NewFromOptions` derives basenames today under implicit repo root. | Process directory, repo root, or require explicit path. | Process directory is consistent with explicit path behavior. | Derive under process directory. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-04 | resolved | Should conflict instructions be runnable from the original process directory? | Existing instructions assume repo root and `.git/MERGE_MSG`. | Process-dir-runnable, root-only, or print both. | Process-dir-runnable is the most complete subdirectory support. | Print commands runnable from invocation directory. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-05 | resolved | Should relative cache paths remain process-directory-relative? | `runtimeCache` currently resolves against `os.Getwd()`. | Keep process-dir-relative, change to repo-root-relative, or require absolute. | Keeping existing behavior avoids unrelated cache semantics changes. | Keep process-dir-relative. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-06 | resolved | Should Braid resolve and use the Git worktree root as internal workdir/config root? | Existing config/Git work assumes `WorkDir` is the root. | Central repo context, top pathspecs everywhere, or process `chdir`. | Central context is explicit and avoids global state. | Resolve central context once and use root for Braid-owned work. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-07 | resolved | Should no-path commands operate on all mirrors or only under the current directory? | Existing no-path commands are repository-wide. | Repository-wide, cwd-scoped, or require path. | Repository-wide preserves current meaning. | Keep repository-wide. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-08 | resolved | Should absolute `local_path` arguments be allowed when inside the worktree? | Stored paths cannot be absolute, but CLI input can be normalized. | Allow inside only, reject all, or store absolute. | Allowing inside paths is useful without compromising config portability. | Allow absolute inputs inside worktree only. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-09 | resolved | Should path containment use logical paths or symlink realpaths? | Git tracks logical paths and existing checks use Git path semantics. | Logical containment, realpath containment, or mix. | Logical containment matches Git and avoids surprising config lookups. | Use logical Git-path normalization. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-10 | resolved | Should user-facing path output be repo-root-relative or process-dir-relative? | Current output, config, and commit subjects use stored mirror paths. | Repo-root-relative, process-dir-relative, or both. | Stable repo-root output is traceable to config and history. | Keep ordinary output repo-root-relative. | Accepted recommendation. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-11 | resolved | Should selectors match containing mirrors or exact mirror roots? | Existing commands use exact config lookup. | Exact match, containing mirror lookup, or defer separate feature. | Exact match avoids hidden expansion and preserves current semantics. | Exact configured mirror-root matching only. | Accepted recommendation. | `00-requirements.md`, `01-path-semantics-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |

## Plan Acceptance

Accepted by the user on 2026-06-20 after iterative plan review returned `Findings: none`.
