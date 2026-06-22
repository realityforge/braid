# Architecture Notes

## Release and Versioning policy

* Releases publish raw binaries plus `SHA256SUMS`.
* Version tags use `vX.Y.Z`; binaries print `X.Y.Z`.

## Git And Tests

- Product code that invokes Git should stay behind `internal/gitexec`.
- Tests must not depend on the user's global Git identity, real Braid cache, or
  network remotes.
- Integration tests should create local upstream/downstream repositories in
  `t.TempDir()`, configure local user identity, and disable GPG signing unless
  the test explicitly covers signing config propagation.

## Non-Negotiable CI Parity

- `.github/workflows/ci.yml` is the source of truth for required checks. Before
  claiming code is ready, read that workflow and run the same checks locally.
- Current required checks are:
  - `bazel run @rules_go//go -- fmt ./...`
  - `git diff --exit-code`
  - `bazel test //...`
  - `bazel run @rules_go//go -- vet ./...`
  - `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
  - `bazel test //integration:braid_integration_test`
- If a CI check cannot be run locally, state exactly which check was not run and
  why. Do not claim the code passes CI unless every required check has passed.
