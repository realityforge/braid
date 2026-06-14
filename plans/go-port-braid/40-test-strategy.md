# Test Strategy

Status: accepted
Last updated: 2026-06-14

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
- Native CI smoke tests prove execution.

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

Draft full gate:

```bash
bazel test //...
bazel build --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64 //cmd/braid:braid
bazel build --platforms=@io_bazel_rules_go//go/toolchain:linux_arm64 //cmd/braid:braid
bazel build --platforms=@io_bazel_rules_go//go/toolchain:darwin_amd64 //cmd/braid:braid
bazel build --platforms=@io_bazel_rules_go//go/toolchain:darwin_arm64 //cmd/braid:braid
bazel build --platforms=@io_bazel_rules_go//go/toolchain:windows_amd64 //cmd/braid:braid
```

Release gate:

```bash
bazel test //...
bazel build <all approved release platforms>
<generate checksums for release binaries>
<native linux smoke test>
<native macOS smoke test>
<native Windows smoke test>
```

Native smoke-test commands will be finalized after CI environment decisions.

## Test Data Hygiene

- No tests should use the user's real Braid cache.
- No tests should use the user's global Git identity.
- No tests should write outside `t.TempDir()` or Bazel test temp directories.
- Tests that need `HOME` or cache paths must set them explicitly.
- Tests must clean up temporary remotes and worktrees unless failure diagnostics need preserved artifacts.
- Tests should verify startup fails clearly when `git --version` reports older than 2.43.0.
- Cache tests must verify default-on behavior, `BRAID_USE_LOCAL_CACHE`, `BRAID_LOCAL_CACHE_DIR`, global `--no-cache`, global `--cache-dir`, and rejection or documented handling of cache flags placed after a subcommand.
