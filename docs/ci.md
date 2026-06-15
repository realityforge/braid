# Continuous Integration

GitHub Actions runs the Go quality gate and the default Bazel test suite on
every pull request and every push to `main`.

## Workflow

The workflow lives in `.github/workflows/ci.yml` and has one job:

- `Go quality and lint` runs formatting, tests, vet, and golangci-lint through
  Bazel. Tests use `bazel test //...` so unit tests, real-Git tests, and the
  executable integration target all run as first-class Bazel targets.

The job installs Bazel, then uses `rules_go` to supply Go. golangci-lint is run
with `bazel run @rules_go//go -- run ...` so CI still has a single automation
entrypoint: Bazel.

## Local Commands

Run the same checks locally before opening a pull request:

```bash
bazel run @rules_go//go -- fmt ./...
bazel test //integration:braid_integration_test
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
```

Formatting should leave the worktree clean:

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
```

## Lint Policy

The lint configuration uses golangci-lint v2 config syntax, keeps the standard
linter set, and enables a small group of high-signal correctness and test
hygiene checks:

- `staticcheck` for broader static analysis.
- `bodyclose`, `durationcheck`, `errchkjson`, `errorlint`, `makezero`,
  `nilerr`, `nilnil`, `unconvert`, and `wastedassign` for common correctness
  mistakes.
- `thelper` and `usetesting` for idiomatic test helpers and temporary test
  resources.
- `misspell` for cheap documentation and comment hygiene.

Avoid enabling every golangci-lint rule by default. Style-heavy checks should
only be added when they catch real defects without making routine contribution
noisy.

Security-specific linters such as `gosec` should be added only with reviewed,
narrow exclusions for intentional Git execution, repository-readable config
files, and test fixtures. Do not add broad suppressions just to make CI green.

## Bazel Gate

This repository remains Bazel-first for release builds and cross-platform
validation. `//integration:braid_integration_test` is the native executable
behavior gate: it runs the Bazel-built `//cmd/braid:braid` binary as a
subprocess against generated local Git repositories.

The pull request workflow runs that target through `bazel test //...` on its
Linux runner. The release gate in `docs/release.md` owns the fixed native
Linux, macOS, and Windows runner matrix for
`bazel test //integration:braid_integration_test`, plus release-platform builds
and packaged-artifact checks.

## GitHub Setup

This is Bazel as the launcher for all automation. It is not fully hermetic
Bazel-native linting: golangci-lint still uses Go module downloads and its own
cache. That tradeoff keeps the setup small while preserving one CI entrypoint.

After pushing this repository to GitHub:

1. Enable GitHub Actions for the repository.
2. Push a branch or open a pull request and confirm the `CI` workflow appears.
3. Protect `main` and require the `Go quality and lint` check before merge.
