# Implementation Plan

Status: accepted
Last updated: 2026-06-14

The user approved this reviewed plan for implementation on 2026-06-14 by requesting the Go+Bazel port proceed from the reviewed plan.

## Phase Sequence

1. Planning and acceptance.
2. Bazel and Go scaffold.
3. Core infrastructure: CLI, Git runner, config, mirror model, path validation.
4. Characterization and fixture strategy.
5. Command slices in risk order: `version`, `setup`, `add`, `diff`, `status`, `update`, `remove`, and `push`.
6. Test suite expansion and parity hardening.
7. Cross-platform build/release packaging.
8. Documentation, cleanup, and final validation.

## Delivery Approach

- Execute one task at a time with minimal diffs.
- Keep Go packages small and boring.
- Prefer standard library packages over third-party dependencies.
- Use Bazel as the required build/test path.
- Keep any optional Go module metadata only if it improves editor/tool compatibility and does not become the source of truth.
- Port behavior in vertical slices: command implementation, focused tests, integration tests, and compatibility docs together.
- Run targeted tests during each slice and full gates before closing a task.
- Record intentional divergences as they are implemented.

## Proposed Repository Layout

```text
MODULE.bazel
MODULE.bazel.lock
.bazelrc
BUILD.bazel
cmd/braid/
  BUILD.bazel
  main.go
internal/
  cli/
  command/
  config/
  gitexec/
  mirror/
  pathcheck/
  testutil/
testdata/
  fixtures/
plans/go-port-braid/
```

## Draft Toolchain Baseline

- Bazel dependency mode: Bzlmod.
- Go rule set: `rules_go` 0.61.1, current in Bazel Central Registry when checked on 2026-06-14.
- Go SDK: 1.26.4, current Go 1.26 patch release when checked on 2026-06-14.
- Cross-build mechanism: `rules_go` platforms with pure-Go builds for `linux_amd64`, `linux_arm64`, `darwin_amd64`, `darwin_arm64`, and `windows_amd64`.
- Cgo policy: off unless a future decision explicitly approves a specific need.
- Runtime Git minimum: 2.43.0.

Package intent:

- `cmd/braid`: process entry point only.
- `internal/cli`: argument parsing, usage, and command dispatch.
- `internal/command`: command implementations and shared command context.
- `internal/config`: `.braids.json` model, load, validate, write.
- `internal/gitexec`: safe Git subprocess runner and typed wrappers for Git operations.
- `internal/mirror`: mirror model, remote/ref naming, revision logic, diff arg construction.
- `internal/pathcheck`: cross-platform path validation.
- `internal/testutil`: Git fixture helpers, hermetic temp repos, golden helpers.

## Dependency Policy

Default to Go standard library:

- `encoding/json`
- `errors`
- `flag` or a small custom parser where subcommand behavior needs it
- `fmt`
- `io/fs`
- `os`
- `os/exec`
- `path/filepath`
- `strings`
- `testing`

Allowed with explicit justification:

- A minimal assertion/diff helper only if standard-library tests become noisy.
- Bazel rule dependencies required for `rules_go`.

Avoid:

- Shell wrappers.
- General-purpose CLI frameworks unless standard-library parsing becomes objectively worse.
- Git libraries such as go-git or libgit2 bindings.
- YAML libraries.
- Global mutable state except for process-level CLI config that is passed through context objects.

## High-Risk Areas

- Git tree/index semantics:
  - Impact: wrong merges, wrong diffs, or data loss.
  - Mitigation: keep delegating to Git CLI; test tree construction, temp indexes, conflicts, single-file mirrors, and path-with-space cases.
- Windows process/path behavior:
  - Impact: quoting bugs, broken paths, output ordering failures.
  - Mitigation: no shell execution; native Windows CI; tests with spaces and backslash-sensitive paths.
- `push` temporary repository behavior:
  - Impact: broken upstream contributions or unexpected user prompts.
  - Mitigation: port late after `add`/`diff`/`update` are stable; add focused tests for tags, branches, filters, alternate object DB, and no-local-change cases.
- Config compatibility:
  - Impact: existing modern users cannot use the Go binary.
  - Mitigation: golden tests for modern `.braids.json`, stable field ordering, schema-level validation, and explicit unsupported legacy diagnostics.
- Overfitting to Ruby internals:
  - Impact: unidiomatic Go and harder maintenance.
  - Mitigation: preserve external behavior; model internals in straightforward Go structs/functions.

## Required Full Gates

Planning default:

```bash
bazel test //...
bazel build --platforms=@rules_go//go/toolchain:linux_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:linux_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid
```

## Task Sequence

1. `T01`: Planning artifacts and decision resolution.
2. `T02`: Bazel/rules_go scaffold with pinned Go SDK and empty `braid version`.
3. `T03`: CLI parser and usage/error conventions.
4. `T04`: Git runner and command wrappers.
5. `T05`: Config and mirror model for modern `.braids.json`.
6. `T06`: Path validation and repository preflight checks.
7. `T07`: Fixture and integration test harness.
8. `T08`: `setup` and shared remote lifecycle.
9. `T09`: `add` command.
10. `T10`: `diff` command.
11. `T11`: `status` command.
12. `T12`: `update` command.
13. `T13`: `remove` command.
14. `T14`: `push` command.
15. `T15`: Removed-command behavior and unsupported legacy-config diagnostics.
16. `T16`: Cross-platform test expansion and release build targets.
17. `T17`: Documentation and migration notes.
18. `T18`: Final gates and plan closeout.

## Decision Log

- Q-01: User selected release target set A. The plan now targets `linux_amd64`, `linux_arm64`, `darwin_amd64`, `darwin_arm64`, and `windows_amd64`; full gates and release matrix were updated accordingly.
- Q-02: User selected the policy of requiring a newer Git from current supported OS package managers. Exact floor is tracked by Q-09.
- Q-03: User selected removal of `upgrade-config`; the command will be unknown rather than implemented.
- Q-04: User selected a new output/help design with behavior parity only; tests should avoid Ruby wording lock-in.
- Q-05: User selected cache-on-by-default for performance, with existing environment variables preserved and CLI override flags to be finalized by Q-10.
- Q-06: User selected Ruby oracle tests during migration only; final required gates are Go/Bazel-only.
- Q-07: User selected raw binaries and checksums with documented manual signing/notarization path for first release artifacts.
- Q-08: User selected root-only execution for v1 with subdirectory execution as a future enhancement.
- Q-09: User selected Git 2.43.0 as the exact minimum, anchored to Ubuntu 24.04 LTS package availability.
- Q-10: User selected global cache flags before the command: `--no-cache` and `--cache-dir <path>`.

## Review Fix Log

- Round 1:
  - Added native release smoke matrix and made CI runner mapping an explicit T16 output.
  - Added per-command preflight matrix so `version`/help can run outside Git repositories.
  - Removed deprecated `update --head` from the Go CLI and required parser tests for rejection.
  - Added cache contract covering env truthiness, CLI precedence, invalid combinations, and no-cache tag behavior.
  - Added cross-platform path validation contract and remote-name collision handling.
  - Added commit, conflict, and push metadata contracts.
- Round 2:
  - Added `git commit --no-verify` to automated commit metadata contract.
  - Expanded T06 closure criteria to require every path validation row and remote-name collision coverage.
  - Updated divergence register approval states for stricter path validation and no-cache tag resolution.
- Round 3:
  - Added update-all contract for branch/tag eligibility, revision-locked skips, and rejected strategy flags without a mirror path.
  - Added explicit no-cache tag and annotated-tag command coverage to `add`, `update`, and `push` tasks.
- Round 4:
  - Made `setup` depend on shared preflight/path validation.
  - Added update-all deterministic ordering and stop-on-first-failure validation.
- Implementation correction:
  - Updated full-gate platform labels from `@io_bazel_rules_go` to the Bzlmod-visible `@rules_go` after verifying `@io_bazel_rules_go` is not visible and `@rules_go` builds.

## Completion Criteria

- All open questions resolved and recorded.
- Plan accepted by user.
- All planned tasks complete.
- Modern `.braids.json` users can run the Go binary without Ruby.
- Required full gates pass with command evidence.
- Native OS smoke tests pass for supported release artifacts.
- Compatibility divergences are documented.
- Working tree is clean or any exception is explicitly documented.
