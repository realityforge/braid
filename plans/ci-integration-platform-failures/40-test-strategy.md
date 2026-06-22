# CI Integration Platform Failures Test Strategy

Status: accepted
Date: 2026-06-22

## Goals

- Prove clean update/sync paths no longer depend on platform-sensitive
  `merge-tree` behavior.
- Prove true update conflicts still produce recoverable Braid conflict state.
- Prove push provenance template capture is reliable on Windows CI.
- Preserve CI parity with `.github/workflows/ci.yml`.

## Targeted Checks

Update and sync behavior:

```bash
bazel test //internal/command:command_test
bazel test //integration:lifecycle_test
bazel test //integration:subdirectory_test
bazel test //integration:sync_test
bazel test //integration:scoped_state_test
```

Required targeted assertions:

- Command-level fast-path tests must make `MergeTreeWrite` fatal if invoked for
  local-equals-base and local-equals-remote cases.
- Coverage must include `sync` after push, where mirror content already matches
  the pushed remote but config revision is stale.
- Coverage must include committed local mirror deletion so an absent
  `HEAD:<mirror>` item remains divergent rather than accidentally fast-pathed.

Merge-tree parsing and conflict behavior:

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test --test_output=errors
bazel test //integration:conflict_test
```

Required targeted assertions:

- Parser tests must distinguish merged tree OID, conflicted path records, and
  informational message records.
- Parser tests must prove the first empty NUL record after the tree OID
  terminates the conflicted-path section.
- Parser tests must cover conflict exit status with an empty structured path
  list.
- Parser tests must cover paths with spaces and duplicate path records.
- Conflict integration tests must assert Braid-owned deterministic
  `CONFLICT: <path>` output, not Git free-form message text.

Executable integration aggregate:

```bash
bazel test //integration:braid_integration_test --test_output=errors --nocache_test_results
```

## Required Full Gate

Run the CI parity commands in this order before claiming readiness:

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
bazel test //integration:braid_integration_test
```

## Cross-Platform Evidence

Local validation is necessary but insufficient because the triggering failures
were platform-specific:

- macOS 15 Arm and macOS 15 Intel failed update/sync integration tests.
- Windows Server 2025 failed the push provenance capture integration test.
- Ubuntu arm64 passed and should remain passing.

After local gates pass, inspect a successor GitHub Actions run and record:

- run URL,
- head SHA,
- status of `Integration (darwin-arm64)`,
- status of `Integration (darwin-amd64)`,
- status of `Integration (windows-amd64)`,
- status of `Integration (linux-arm64)`,
- status of `Go quality and lint`.

The plan may only be marked complete when the named target failures are fixed:

- `Integration (darwin-arm64)` no longer fails the clean update/sync and
  conflict-output assertions from run `27927786035`.
- `Integration (darwin-amd64)` no longer fails the same clean update/sync and
  conflict-output assertions from run `27927786035`.
- `Integration (windows-amd64)` no longer fails
  `TestExecutablePushProvenanceTemplateTouchesGitDefaultTemplate` with `The
  system cannot find the file specified.`

Residual CI failures may be documented only when they are unrelated to those
target failures.

## Residual Risk

- If local Git cannot reproduce Git `2.54.0` macOS behavior, remote CI remains
  the decisive validation for the macOS fix.
- If Windows editor helper behavior depends on Git-for-Windows invocation
  details, the Windows CI job remains the decisive validation for that fix.
