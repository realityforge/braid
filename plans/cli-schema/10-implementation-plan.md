# CLI Schema Implementation Plan

## Phase Sequence

1. Introduce and validate the CLI schema while preserving existing parsing.
2. Move parser and usage generation onto the schema.
3. Move completion grammar onto the schema and retain runtime providers in
   `internal/command`.
4. Generate structural unit and executable contract cases from the schema;
   retain focused provider and emitted-script tests.
5. Run CI parity, inspect alignment, commit implementation, and remove this
   plan tree in a cleanup commit.

## High-Risk Areas

- Exact diagnostics and `Invocation` construction may change during parser
  migration. Preserve existing focused parser tests and migrate incrementally.
- `add` has a conditional `:source` grammar and `diff` has passthrough syntax.
  Represent both explicitly and test each transition.
- Test generation must not merely restate implementation decisions. Validate
  schema invariants independently and execute generated cases through both the
  in-process app and compiled binary.

## Required Full Gates

- `bazel run @rules_go//go -- fmt ./...`
- formatting produces no additional diff beyond intended changes
- `bazel test --test_env=PATH //...`
- `bazel run @rules_go//go -- vet ./...`
- `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
- `bazel test --test_env=PATH //integration/...`

## Completion Criteria

- One schema owns the CLI contract.
- Structural completion coverage follows schema changes automatically at unit
  and executable integration levels.
- Focused behavior and regression tests pass.
- Implementation is committed and the plan tree is removed separately.
