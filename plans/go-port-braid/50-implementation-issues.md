# Implementation Issues

Status: active
Last updated: 2026-06-14

## PI-01: Bzlmod rules_go Platform Label

- status: resolved
- discovered_in: T02
- context: The accepted plan listed full-gate cross-build platforms under `@io_bazel_rules_go//go/toolchain:*`.
- evidence:
  - `bazel build --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64 //cmd/braid:braid` failed because no repository named `@io_bazel_rules_go` is visible from the Bzlmod root.
  - `bazel build --platforms=@rules_go//go/toolchain:linux_amd64 //cmd/braid:braid` passed.
- why_it_matters: The required full-gate commands must be executable exactly as recorded in the plan and task board.
- response: Updated the active plan full-gate commands and release target labels to the Bzlmod-visible `@rules_go//go/toolchain:*` form.
- tracking_task_ids: T02

## PI-02: Implementation Review Parity Evidence Gaps

- status: resolved
- discovered_in: iterative-plan-implementation-review round 1
- context: `01-command-parity.md` had completed status, but the acceptance checklist was still unchecked and the implemented test evidence did not explicitly cover all high-risk `diff` and `push` mirror variants.
- evidence:
  - Added `diff` integration coverage for tag, revision-locked, subdirectory, and path-with-spaces mirrors.
  - Added `push` integration coverage for revision-locked mirrors requiring `--branch` and pushing successfully to an explicit branch.
  - Added short production comments around temporary index composition, `merge-recursive`, temp-repo push assembly, alternates, and sparse checkout.
  - `bazel test //internal/command:command_test` passed.
  - `bazel test //internal/gitexec:gitexec_test` passed.
  - `bazel test //...` passed.
  - All required `@rules_go//go/toolchain:*` release target builds passed.
- why_it_matters: The plan promises stronger parity and maintainability evidence than a direct Ruby test port, so completed status must be backed by explicit tests and readable non-obvious Git choices.
- response: Filled the missing tests, updated the command parity checklist, clarified the revision/tag branch requirement diagnostic, and recorded review evidence in the task board.
- tracking_task_ids: T10, T14, T18

## PI-03: Windows Push Alternates Used Relative Git Object Paths

- status: resolved
- discovered_in: user Windows smoke test after initial Go port completion
- context: `push` assembles a temporary repository and writes the source repository object directory into `.git/objects/info/alternates`.
- evidence:
  - Windows run failed with `unable to normalize alternate object path: .../.git/objects/.git\objects`.
  - `git rev-parse --git-path objects` can return a relative path such as `.git\objects`.
  - Git resolves relative alternates from the temporary object database, not from the source worktree.
  - Commit `aad9cf0` normalizes alternates to an absolute slash path and adds regression coverage.
- why_it_matters: Broken alternates prevent `braid push` from seeing the source object database and make the temporary repository fail before it can create the pushed commit.
- response: Added `alternateObjectPath`, made alternates non-empty, absolute, and slash-normalized, and covered the real `NewApp()` default-workdir path.
- tracking_task_ids: WPH-01

## PI-04: Git-Returned File Paths Need Central Normalization Before OS Writes

- status: resolved
- discovered_in: Windows path audit after PI-03
- context: `update` writes merge conflict subjects to Git's `MERGE_MSG` path from `git rev-parse --git-path MERGE_MSG`.
- evidence:
  - `UpdateHandler.writeMergeMessage` joins a Git-returned path with `workDir` but does not force an absolute native OS path.
  - The code has the same relative-Git-path shape that caused PI-03, though the write target is local to the repository rather than a Git alternates file.
- why_it_matters: On Windows, Git may return backslash paths or relative paths. Every Git path that is later passed to `os.ReadFile`, `os.WriteFile`, or similar APIs should be normalized consistently before use.
- response: Added `gitRepoOSPath` in the command layer, kept `RepoFilePath` as
  a raw Git wrapper, used the helper before writing update conflict
  `MERGE_MSG`, and reused it for push alternates while preserving slash output
  for Git alternates.
- resolution_evidence:
  - `bazel test //internal/cli:cli_test //internal/mirror:mirror_test //internal/command:command_test`
  - `bazel test //...`
  - `bazel run @rules_go//go -- vet ./...`
  - `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
  - `bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid`
- tracking_task_ids: WPH-02

## PI-05: Native Windows Paths At The CLI Boundary Are Ambiguous

- status: resolved
- discovered_in: Windows path audit after PI-03
- context: Braid's config format deliberately stores portable slash-separated mirror paths, but Windows users naturally type backslash paths and absolute drive paths.
- evidence:
  - `pathcheck.ValidateLocal` and `ValidateUpstream` correctly reject backslashes, drive paths, and UNC paths for portable mirror paths.
  - CLI lookup paths such as `braid status vendor\repo` are not normalized before config lookup, so they will not match `vendor/repo`.
  - `mirror.defaultLocalPath` uses `path.Base`, so `braid add C:\Users\peter\repo` can derive a non-portable local mirror path from a native Windows local repository path.
- why_it_matters: The current rules protect `.braids.json`, but they can surprise Windows users at the CLI and can make local-repository workflows fail before a helpful diagnostic.
- response: CLI local mirror path arguments now normalize `\` to `/` before
  validation or config lookup. Explicit `add` local paths use the same
  canonicalization. Native Windows local upstream repository strings now derive
  default mirror paths from their basename while preserving the original
  upstream value for Git.
- resolution_evidence:
  - Parser tests cover `add`, `update`, `remove`, `diff`, `push`, `setup`, and
    `status` local path arguments with backslashes.
  - Command integration tests cover `add` storing `vendor/native` from
    `vendor\native` and `status` selecting `vendor/basic` via `vendor\basic`.
  - Mirror unit tests cover Windows drive, slash, trailing separator, and UNC
    local repository path default derivation.
- tracking_task_ids: WPH-03, WPH-04

## PI-06: Cache Directory Defaults And Cache Keys Are Not Windows-Hardened

- status: resolved
- discovered_in: Windows path audit after PI-03
- context: Cache configuration is performance-critical and enabled by default.
- evidence:
  - `ResolveCache` uses `HOME` for the default cache root and falls back to `.braid/cache` when `HOME` is unavailable.
  - Windows commonly supplies `USERPROFILE`, `LOCALAPPDATA`, or APIs exposed by `os.UserCacheDir` rather than `HOME`.
  - `expandHome` handles `~` and `~/...`, but not `~\...`.
  - `CachePath` replaces `/`, `:`, and `@`, but not all Windows-invalid filename characters and not backslashes.
- why_it_matters: Cache behavior should be predictable on Windows and cache paths should be valid regardless of URL or local-path style.
- response: Default cache location now uses `os.UserCacheDir()` with a `braid`
  child directory when no explicit override is set, then falls back to
  `~/.braid/cache` when the OS cache directory is unavailable. Cache URL keys
  are SHA-256 directory names, and `~\...` cache paths expand through the same
  home-directory path as `~/...`.
- resolution_evidence:
  - Cache tests cover precedence, OS cache default, `USERPROFILE` fallback,
    `~\...` expansion, stable SHA-256 cache keys, and URL inputs containing
    Windows-invalid filename characters.
  - `bazel test //internal/command:command_test`
- tracking_task_ids: WPH-05

## PI-07: Upstream Tree Contents May Contain Windows-Incompatible Paths

- status: resolved
- discovered_in: Windows path audit after PI-03
- context: Braid validates the requested mirror root and upstream `--path`, but it delegates checkout of the selected upstream tree to Git.
- evidence:
  - Local mirror path validation rejects Windows-reserved basenames and trailing dot/space path elements.
  - Upstream tree contents under a valid root are not enumerated before checkout.
  - Git will fail if it cannot materialize upstream files on Windows.
- why_it_matters: This may be acceptable as a Git-level failure, but release-quality Windows support should decide whether Braid provides a clearer preflight diagnostic.
- response: Accepted the documented boundary: Braid validates configured mirror
  paths but does not enumerate every upstream tree entry before checkout. The
  README documents that Git reports checkout failures for upstream filenames
  that cannot be materialized on the current OS, and the Windows release smoke
  script now covers a normal upstream tree containing spaces in path elements.
- tracking_task_ids: WPH-06
