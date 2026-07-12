# Architecture Notes

## Release and Versioning policy

* Releases publish raw binaries plus `SHA256SUMS`.
* Version tags use `vX.Y.Z`; binaries print `X.Y.Z`.

## Git And Tests

- All changes must land through pull requests; do not push changes directly to
  the default branch.
- Product code that invokes Git should stay behind `internal/gitexec`.
- Tests must not depend on the user's global Git identity, real Braid cache, or
  network remotes.
- Integration tests should create local upstream/downstream repositories in
  `t.TempDir()`, configure local user identity, and disable GPG signing unless
  the test explicitly covers signing config propagation.
- Any change to the CLI interface, including commands, aliases, options,
  positional arguments, or help forms, must include both unit-level and
  integration completion tests. The tests must exercise completion at every
  valid position affected by the change, including before and after positional
  arguments where options are accepted.

## Non-Negotiable CI Parity

- `.github/workflows/ci.yml` is the source of truth for required checks. Before
  claiming code is ready, read that workflow and run the same checks locally.
- Current required checks are:
  - `bazel run @rules_go//go -- fmt ./...`
  - `git diff --exit-code`
  - `bazel test //...`
  - `bazel run @rules_go//go -- vet ./...`
  - `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
  - `bazel test //integration/...`
- If a CI check cannot be run locally, state exactly which check was not run and
  why. Do not claim the code passes CI unless every required check has passed.
