# Windows Path Hardening Plan

Status: implemented
Last updated: 2026-06-14

## Goal

Make Windows path behavior deliberate, tested, and release-ready without weakening
the portable `.braids.json` contract. The plan addresses issues found during the
post-port Windows path audit and the resolved Windows `push` alternates failure.

## Scope

In scope:

- Git-returned paths that Braid later passes to OS file APIs.
- CLI path inputs for local mirror selection and local repository URLs.
- Cache directory defaults and cache key generation on Windows.
- Test coverage that exercises real CLI defaults rather than only injected
  absolute `WorkDir` values.
- Documentation for any Windows path behavior that remains intentionally strict.

Out of scope:

- Supporting execution from Git subdirectories. That remains covered by
  `docs/future-subdirectory-execution.md`.
- Changing `.braids.json` to store native OS separators.
- Supporting historic legacy config formats.
- Replacing Git CLI behavior with a Git library.

## Issue Map

| Issue | Status | Summary | Planned Task |
| --- | --- | --- | --- |
| PI-03 | resolved | `push` alternates used relative Git object paths on Windows | WPH-01 |
| PI-04 | resolved | Git-returned paths need central normalization before OS writes | WPH-02 |
| PI-05 | resolved | Native Windows CLI path inputs are ambiguous | WPH-03, WPH-04 |
| PI-06 | resolved | Cache defaults and cache keys are not Windows-hardened | WPH-05 |
| PI-07 | resolved | Upstream tree contents may contain Windows-incompatible paths | WPH-06 |

## Design Principles

- Keep stored mirror paths portable and slash-separated.
- Normalize at explicit boundaries: CLI input, Git output used as OS paths, and
  cache filesystem paths.
- Pass repository paths to Git in Git's slash-based path syntax unless the value
  is explicitly an OS filesystem path.
- Prefer small helpers with direct tests over scattered ad hoc conversions.
- Avoid compatibility shims for legacy config; this is Windows hardening for the
  modern Go port only.

## Task Plan

### WPH-01: Record And Preserve The Push Alternates Fix

- status: completed
- implementation:
  - `push` writes absolute slash-normalized alternates paths.
  - Regression test runs `add` and `push` through `NewApp()` with the default
    process workdir.
- evidence:
  - `bazel test //internal/command:command_test`
  - `bazel test //...`
  - `bazel run @rules_go//go -- vet ./...`
  - `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
  - `bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid`

### WPH-02: Normalize Git Paths Before OS File APIs

- status: completed
- implementation:
  - Add a helper that converts Git-returned paths into native absolute OS paths
    relative to the command workdir when needed.
  - Use it for `update` conflict `MERGE_MSG` writes.
  - Keep `RepoFilePath` as a raw Git wrapper and normalize only at the command
    layer before OS file APIs.
  - Reuse the helper for `push` alternates, preserving the alternates-specific
    requirement that the final path written to Git is slash-normalized.
- tests:
  - Unit-test relative `.git/MERGE_MSG`, Windows-style `.git\\MERGE_MSG`, and
    already-absolute path cases.
  - Existing conflict integration coverage verifies `MERGE_MSG` behavior on the
    native test host.
- acceptance:
  - No direct OS write uses an unnormalized Git-returned path.
  - Existing conflict behavior remains unchanged on macOS/Linux.

### WPH-03: Normalize CLI Local Mirror Path Inputs

- status: completed
- decision:
  - Accepted WPH-Q01 recommendation. Command path arguments such as
    `status vendor\repo` should normalize to `vendor/repo` at the CLI boundary.
- approach:
  - Normalize local mirror selector arguments by replacing `\` with `/` before
    config lookup, while keeping config storage slash-only and still rejecting
    backslashes in persisted `.braids.json` paths. This is deliberately not
    limited to `filepath.ToSlash`, because tests on non-Windows hosts must still
    exercise Windows-style input strings.
- affected commands:
  - `update <local_path>`
  - `remove <local_path>`
  - `diff [local_path]`
  - `push <local_path>`
  - `setup [local_path]`
  - `status [local_path]`

### WPH-04: Support Native Windows Local Repository Inputs

- status: completed
- decision:
  - Accepted WPH-Q02 recommendation. `braid add C:\path\to\repo` should be
    accepted as a local upstream repository path on Windows.
- approach:
  - Preserve the original upstream string for Git remote configuration.
  - Normalize only the derived default local mirror path, using the native path
    basename when the upstream string is a local filesystem path.
- tests:
  - Unit-test default local path derivation for slash URLs, backslash local
    paths, paths with trailing separators, and `.git` suffixes.
  - Native Windows smoke should include adding from a local repository path.

### WPH-05: Harden Cache Directory And Cache Key Behavior

- status: completed
- decision:
  - Accepted WPH-Q03 recommendation. The default cache root should use the OS
    user cache directory when available.
- approach:
  - Replace character-substitution cache keys with a deterministic SHA-256
    cache key.
  - Use `os.UserCacheDir()` when no explicit cache location is configured, then
    store Braid cache data under that root.
  - Preserve the existing override precedence: `--no-cache`, `--cache-dir`,
    `BRAID_USE_LOCAL_CACHE`, `BRAID_LOCAL_CACHE_DIR`, then default cache root.
  - Support `~\...` expansion if native Windows-style cache paths are accepted.
- acceptance:
  - Cache path generation cannot produce Windows-invalid filenames for any URL.
  - Existing `BRAID_USE_LOCAL_CACHE`, `BRAID_LOCAL_CACHE_DIR`, `--no-cache`, and
    `--cache-dir` precedence remains intact.
  - Tests cover URL/path inputs containing `\`, `?`, `*`, `"`, `<`, `>`, and `|`.

### WPH-06: Document Upstream Tree Windows Compatibility Boundary

- status: completed
- decision:
  - Accepted WPH-Q04 recommendation. Do not add a full upstream tree preflight
    in the first Windows hardening pass.
- approach:
  - Rely on Git checkout errors for upstream files that cannot be materialized
    on Windows.
  - Document the boundary in `README.md`.
  - Update the native Windows smoke script to cover a local repository path,
    mirror paths with spaces, backslash selectors, cache enabled/default,
    `--no-cache`, `diff`, `update`, `push`, `remove`, and upstream path
    elements containing spaces.
- rationale:
  - Full tree enumeration can be expensive for large mirrors and duplicates
    Git's own checkout validation. It may be better as a later diagnostic
    enhancement after native Windows CI exists.

## Verification Plan

Run after each implementation slice:

```bash
bazel run @rules_go//go -- fmt ./...
bazel test //internal/command:command_test
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
```

Run before closing the hardening plan:

```bash
bazel test //...
bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid
```

Native Windows evidence required before release:

- `braid version`
- `braid add` from a local Windows path upstream
- `braid status`, `diff`, `update`, `remove`, and `push` using a mirror path
  with spaces
- Cache enabled by default and cache disabled with `--no-cache`

## Implementation Evidence

- `bazel run @rules_go//go -- fmt ./...`: pass
- `bazel test //internal/cli:cli_test //internal/mirror:mirror_test //internal/command:command_test`: pass
- `bazel test //...`: pass
- `bazel run @rules_go//go -- vet ./...`: pass
- `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`: pass, 0 issues
- `bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid`: pass

## Open Questions

### WPH-Q01: CLI Mirror Path Separator Policy

- status: resolved
- question: Should command local path arguments accept native Windows
  backslashes and normalize them to slash-separated mirror paths?
- recommended_answer: Yes. Normalize CLI selector inputs to slash paths before
  config lookup, while keeping `.braids.json` slash-only and still rejecting
  backslashes inside stored config paths.
- user_decision: Accepted recommendation. CLI mirror path selector arguments
  should accept native Windows backslashes and normalize to portable slash paths
  before lookup.

### WPH-Q02: Local Repository URL Policy

- status: resolved
- question: Should `braid add C:\path\to\repo` be accepted as a local upstream
  repository URL on Windows?
- recommended_answer: Yes. Preserve the original URL/path for Git, but derive
  the default local mirror path from the native path basename in a portable way.
- user_decision: Accepted recommendation. Native Windows local upstream paths
  should be accepted, with Git receiving the original input and Braid deriving a
  portable default mirror path from the local path basename.

### WPH-Q03: Default Cache Root Policy

- status: resolved
- question: Should default cache location move to the OS user cache directory
  when available?
- recommended_answer: Yes, for new Go behavior. Keep explicit env vars and CLI
  flags as higher-precedence overrides.
- user_decision: Accepted recommendation. Default cache location should use the
  OS user cache directory when available, while preserving explicit CLI and
  environment overrides.

### WPH-Q04: Upstream Tree Preflight Policy

- status: resolved
- question: Should Braid enumerate upstream tree contents to detect
  Windows-incompatible filenames before checkout?
- recommended_answer: No for the first hardening pass. Let Git report checkout
  failures and revisit if native Windows users hit unclear diagnostics.
- user_decision: Accepted recommendation. Do not add a full upstream tree
  compatibility preflight in the first Windows hardening pass.
