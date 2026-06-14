# Continuous Integration

GitHub Actions runs the fast Go quality gate on every pull request and every
push to `main`.

## Workflow

The workflow lives in `.github/workflows/ci.yml` and has one job:

- `Go quality and lint` runs formatting, tests, vet, and golangci-lint through
  Bazel. Tests use `bazel test //...` so they run as first-class Bazel targets.

The job installs Bazel, then uses `rules_go` to supply Go. golangci-lint is run
with `bazel run @rules_go//go -- run ...` so CI still has a single automation
entrypoint: Bazel.

## Local Commands

Run the same checks locally before opening a pull request:

```bash
bazel run @rules_go//go -- fmt ./...
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

The lint configuration is intentionally small. It uses golangci-lint v2 config
syntax, keeps the standard linter set, and explicitly enables `staticcheck`.
Add more linters only when the signal is strong enough to keep the tool quiet
and useful for routine contribution.

## Bazel Gate

This repository remains Bazel-first for release builds and cross-platform
validation. The GitHub Actions Go quality gate checks source-level Go health;
the release gate in `docs/release.md` still owns Bazel platform builds and
native release smoke tests.

## GitHub Setup

This is Bazel as the launcher for all automation. It is not fully hermetic
Bazel-native linting: golangci-lint still uses Go module downloads and its own
cache. That tradeoff keeps the setup small while preserving one CI entrypoint.

After pushing this repository to GitHub:

1. Enable GitHub Actions for the repository.
2. Push a branch or open a pull request and confirm the `CI` workflow appears.
3. Protect `main` and require the `Go quality and lint` check before merge.
