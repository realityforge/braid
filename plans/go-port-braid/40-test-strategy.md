# Test Strategy

Status: completed
Last updated: 2026-06-15

## Test Philosophy

The Go port should not merely translate the Ruby tests. The Ruby suite is the seed corpus. The Go test suite should prove command behavior, Git side effects, cross-platform safety, and intentional divergences.

Tests should be readable to maintainers who are new to Go. Prefer table tests, explicit fixtures, and helper functions with narrow responsibilities.

## Test Layers

### Unit Tests

Targets:

- CLI parsing and usage errors.
- Per-command preflight requirements.
- Config load/validate/write.
- Mirror ref/remote naming.
- Path validation.
- Cache env/flag precedence.
- Git command argv construction.
- Error wrapping and diagnostics.

Expectations:

- Fast and hermetic.
- No real network.
- No shell execution.
- Table-driven where cases are numerous.

### Real-Git Integration Tests

Targets:

- Commands operating in temporary repositories.
- Actual commits, remotes, tree objects, temp indexes, merges, conflicts, and diffs.
- OS-specific behavior through native CI.

Expectations:

- Use `internal/testutil` to create repos with local user identity and disabled GPG signing.
- Use local repositories as upstream remotes.
- Keep temp directories isolated.
- Avoid relying on global user Git config.

### Executable Integration Tests

Targets:

- The Bazel-built `//cmd/braid:braid` executable running as a subprocess.
- Full command lifecycles against synthesized upstream and downstream repository trees.
- Explicit per-test environments, including isolated home and cache directories.
- Exact post-command assertions for file trees, `.braids.json`, cache use, stdout/stderr, exit codes, remotes, and Git commit metadata.

Expectations:

- Add a default `bazel test //...` target, not a manual-only target:
  `//integration:braid_integration_test`.
- Define `integration/BUILD.bazel` with a Go test target that has
  `data = ["//cmd/braid:braid"]`.
- Locate the executable through Bazel runfiles and apply the platform executable
  suffix where needed.
- Do not import `braid/cmd/braid` or `braid/internal/command`; the integration
  boundary is the process interface.
- Use local repositories and generated directory trees under `t.TempDir()`.
- Use one shared subprocess helper for both Git setup/assertion commands and
  Braid commands.
- Build subprocess environments from an explicit allow-list. Required entries
  include `PATH`/`Path`, isolated `HOME`, isolated `USERPROFILE`,
  Unix cache/config homes where applicable, Windows process essentials where
  applicable, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_NOSYSTEM=1`,
  `GIT_TERMINAL_PROMPT=0`, deterministic locale, editor variables for push
  tests, and `BRAID_LOCAL_CACHE_DIR` except in focused cache precedence tests.
- Use an explicit `BRAID_LOCAL_CACHE_DIR` in main scenarios, with separate
  tests for cache env and flag precedence.
- Keep tests serial in the first pass.
- Future TODO: after the executable integration suite is stable, evaluate adding `t.Parallel()` to selected scenarios if runtime becomes a problem.

Executable scenario ownership:

- Primary lifecycle: `version`, `add`, `status`, `diff`, `update`, `push`, and
  `remove` through the built binary, asserting process output, exit code, file
  tree, config, cache, remotes, and commit metadata after each step.
- Setup/cache: `setup`, `setup --force`, default cache use, `--no-cache`, and
  `--cache-dir` subprocess behavior.
- Path variants: at least one subprocess scenario for subdirectory mirrors,
  single-file mirrors, or paths with spaces; deeper branch/tag/revision/path
  matrices remain owned by the existing in-process command tests.
- Failure paths: missing config, unsupported legacy `.braids`, wrong working
  directory or subdirectory execution, invalid cache flag combinations, failed
  add rollback, and merge conflict behavior.

Native platform gate:

- The platform-of-interest execution gate is `bazel test
  //integration:braid_integration_test` on fixed native Linux, macOS, and
  Windows runners.
- Release behavior coverage should use that Bazel target rather than duplicated
  release smoke scripts.
- Release packaging checks stay artifact-focused: copied binary launches,
  `version` prints successfully, checksums match the uploaded files, executable
  bits are correct where applicable, and macOS signing/notarization checks pass
  when signing is performed.
- Updating `docs/release.md` and release/CI automation to reflect this split is
  part of implementing the executable integration suite.

### Characterization Tests

Targets:

- High-risk parity flows compared against Ruby Braid during migration.
- Example flows: add/update/diff/push branch mirror, subdirectory mirror, single-file mirror, conflict update.

Expectations:

- Migration-only; final required gates must not depend on Ruby.
- Record any discovered divergence in `30-compatibility-matrix.md`.

### Golden Tests

Targets:

- `.braids.json` formatting and field ordering.
- Selected command output categories where diagnostics must remain stable.
- Selected diff output.

Expectations:

- Golden updates must be reviewed.
- Avoid golden testing full help text; test only core usage shape and command/flag presence.

### Security and Negative Tests

Targets:

- Unsafe local paths.
- Arguments with spaces and shell metacharacters.
- Unknown commands and flags.
- Unsupported legacy config.
- Deprecated `update --head`.
- Git failures and ambiguous revisions.
- Running outside a Git worktree or from the wrong directory.
- Running from a subdirectory of a Git worktree must fail clearly in v1.
- Invalid cache flag combinations.
- Path edge cases in the requirements path validation table.

Expectations:

- Assert no shell interpolation by using args that would be dangerous in a shell.
- Assert no partial mutation after early validation failures.

### Cross-Platform Tests

Targets:

- Linux/macOS/Windows native execution.
- Path separators and spaces.
- CRLF and file mode behavior where platform supports it.
- Windows process output ordering.

Expectations:

- Bazel cross-builds prove compilation only.
- Native Bazel integration gates prove execution on each platform of interest.

## Fixture Strategy

Initial seed:

- Study `vendor/braid/spec/fixtures` and port the minimum useful set into `testdata/fixtures`.
- Keep Go-owned fixtures small and named by behavior.
- Do not require Ruby fixture structure long term.

Suggested fixture groups:

- `upstream-basic-v1`, `upstream-basic-v2`, `upstream-basic-conflict`
- `upstream-subdir`
- `upstream-single-file`
- `upstream-path-with-spaces`
- `downstream-clean`
- `downstream-modern-config`
- `downstream-future-config`
- `downstream-legacy-config-unsupported`

## Coverage Targets

Do not use a raw coverage percentage as the primary quality signal. For this tool, behavioral matrix coverage matters more than line coverage.

Required behavior coverage:

- Every command has parser, success, and at least one failure test.
- `version`, help, usage, and parse errors run without Git or a Git worktree.
- Every command that mutates Git has clean-repo and dirty-repo tests.
- Every mirror type has add, diff, update, remove, and status coverage where applicable.
- `update` all-mirror tests cover mixed branch, tag, and revision-locked mirrors, rejected strategy flags without a mirror path, deterministic config path order, and stop/report-on-first-failure behavior.
- `push` has branch, explicit branch, tag rejection, no-local-change, and not-up-to-date tests.
- `push` has identity/signing propagation, editor cancellation, and temp cleanup tests.
- No-cache tests cover tag and annotated-tag flows for `add`, `update`, and tag-with-explicit-branch `push`.
- `add`, `update`, and `remove` have failing-hook tests proving automated commits use `--no-verify`.
- Config has modern valid, future version, malformed JSON, unknown fields, missing required fields, and unsupported legacy tests.

Optional coverage report:

```bash
bazel coverage //...
```

This can become a full gate after the Bazel/Go scaffold proves stable.

## Required Full Gates

Required full gate:

```bash
bazel test //...
bazel build --platforms=@rules_go//go/toolchain:linux_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:linux_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid
```

Release gate:

```bash
bazel test //...
bazel build <all approved release platforms>
<generate checksums for release binaries>
<native Linux bazel test //integration:braid_integration_test>
<native macOS bazel test //integration:braid_integration_test>
<native Windows bazel test //integration:braid_integration_test>
<packaged artifact version/checksum/executable/signing checks>
```

Concrete artifact names, fixed native runner labels, checksum commands,
native integration-test commands, and packaged artifact checks must be
documented in `docs/release.md`. Behavior-heavy release smoke scripts should
be removed after `//integration:braid_integration_test` owns that coverage.

## Test Data Hygiene

- No tests should use the user's real Braid cache.
- No tests should use the user's global Git identity.
- No tests should write outside `t.TempDir()` or Bazel test temp directories.
- Tests that need `HOME` or cache paths must set them explicitly.
- Tests must clean up temporary remotes and worktrees unless failure diagnostics need preserved artifacts.
- Tests should verify startup fails clearly when `git --version` reports older than 2.39.0.
- Cache tests must verify default-on behavior, `BRAID_USE_LOCAL_CACHE`, `BRAID_LOCAL_CACHE_DIR`, global `--no-cache`, global `--cache-dir`, and rejection or documented handling of cache flags placed after a subcommand.
