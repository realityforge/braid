# Per-File Bazel Test Targets Requirements

Status: accepted
Date: 2026-06-22

## Mission

Replace broad aggregate Go test targets with first-class per-file Bazel
`go_test` targets so focused local test runs can address one test file at a
time, while CI continues to run the full required suite through Bazel target
patterns.

## Evidence From Existing Code

- `internal/command/BUILD.bazel` currently has one `go_test` target,
  `command_test`, covering 11 files and 149 `Test*` functions.
- `internal/command` test files are coupled through package-level helpers:
  `status_test.go`, `sync_test.go`, `update_test.go`, `remove_test.go`,
  `diff_test.go`, and `push_test.go` use helpers defined in other test files.
- `integration/BUILD.bazel` currently has one aggregate
  `braid_integration_test` target plus per-file targets tagged `manual`.
- `.github/workflows/ci.yml` already runs `bazel test //...` in the Go quality
  job and runs `bazel test //integration:braid_integration_test` in the
  platform matrix integration job.
- `bazel test //... --nobuild` currently reports 10 test targets, which confirms
  the existing `manual` integration targets are excluded from wildcard test
  expansion.
- There is no repository `.bazelrc` changing test-target selection behavior.

## Scope

In scope:

- Convert affected aggregate Go test targets to one Bazel `go_test` target per
  real feature `*_test.go` file that contains runnable tests.
- Remove `manual` tags from per-file test targets that should participate in
  wildcard CI selection.
- Remove aggregate test targets that duplicate the same test files.
- Extract shared test helpers so each per-file target compiles independently.
- Update CI to use Bazel test patterns instead of aggregate target labels.
- Update `AGENTS.md` CI parity notes if required check labels change.

Out of scope:

- Changing production behavior.
- Reorganizing tests beyond what is needed for independent per-file compilation.
- Splitting individual Go test functions into smaller files.
- Adding fallback aggregate targets for compatibility.
- Introducing non-Bazel test runners.

## Locked Decisions

- Per-file targets must not use `tags = ["manual"]`.
- The aggregate target for a converted package should be removed, not kept as a
  compatibility alias.
- CI should select test suites through target patterns.
- `bazel test //...` must remain a valid full unit/integration-on-current-host
  pattern.
- Shared helper extraction should keep helper code test-only.

## Command Surface And Behavior Expectations

Expected focused command package targets:

```bash
bazel test //internal/command:add_test
bazel test //internal/command:cache_test
bazel test //internal/command:diff_test
bazel test //internal/command:path_test
bazel test //internal/command:preflight_test
bazel test //internal/command:push_test
bazel test //internal/command:remove_test
bazel test //internal/command:setup_test
bazel test //internal/command:status_test
bazel test //internal/command:sync_test
bazel test //internal/command:update_test
```

Expected full-pattern commands:

```bash
bazel test //...
bazel test //internal/command/...
```

If integration is in scope, expected focused integration targets remain named
after their files:

```bash
bazel test //integration:lifecycle_test
bazel test //integration:sync_test
bazel test //integration:subdirectory_test
bazel test //integration:setup_failure_test
bazel test //integration:scoped_state_test
bazel test //integration:conflict_test
```

## Helper Extraction Requirements

- Shared command-test helpers should move out of feature-specific test files.
- The support shape must preserve access to unexported `internal/command`
  symbols where tests currently need same-package access.
- Preferred implementation for `internal/command` is a test-only support
  `go_library` that embeds `:command` and owns the helper-only
  `test_support_test.go` source.
- Helper-only `_test.go` files must not get standalone `go_test` labels unless
  they contain runnable `Test*`, `Benchmark*`, `Fuzz*`, or `Example*`
  functions.
- Keep `test_support_test.go` as a `_test.go` source so non-Bazel Go tooling
  does not pull `testing` helpers into the production `internal/command`
  package.
- Do not move command test helpers into production libraries unless a concrete
  test target requires it and there is no same-package test-only alternative.
- Keep helper extraction minimal; do not rewrite test bodies except to compile
  after helper movement.

## Timeout Requirements

- Preserve the current command-package safety margin by setting `timeout =
  "long"` on command per-file targets unless measured focused runs justify a
  narrower timeout before implementation is complete.
- Integration per-file targets should keep the existing `size = "medium"`
  setting.

## Quality Gates

Required local CI parity gates from `.github/workflows/ci.yml`:

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
bazel test //integration/...
```

Targeted gates during implementation:

```bash
bazel query 'kind("go_test rule", //internal/command:*)'
bazel test //internal/command:add_test
bazel test //internal/command:push_test
bazel test //internal/command:sync_test
bazel test //internal/command/...
bazel test //...
```

If integration is in scope:

```bash
bazel query 'kind("go_test rule", //integration:*)'
bazel test //integration:lifecycle_test
bazel test //integration/...
```

## Known Intentional Divergences

- Target labels used by developers and CI will change from aggregate labels to
  per-file labels.
- `bazel test //internal/command:command_test` will stop working if
  `internal/command` is converted and the aggregate is removed.
- If integration is converted, `bazel test //integration:braid_integration_test`
  will stop working and CI must use a pattern.

## Open Questions Register

### Q-01

- status: resolved
- question: Should this change apply only to `internal/command`, or should it
  also convert the existing `integration` aggregate/manual-target pattern?
- context: The original pain point is `//internal/command:command_test`, but the
  requested changes mention removing `manual` tags, changing CI to use patterns,
  and removing aggregate targets. Those requirements directly match the existing
  `integration/BUILD.bazel` shape, while `internal/command` currently has no
  manual tags.
- options:
  - Convert only `internal/command`.
  - Convert both `internal/command` and `integration`.
  - Convert every package with a Go test aggregate, even packages that currently
    have only one test file.
- tradeoffs:
  - `internal/command` only is the smallest change but leaves the repo with two
    competing test-target conventions and does not remove any existing manual
    tags.
  - `internal/command` plus `integration` matches the stated aggregate/manual/CI
    goals and keeps scope bounded to packages where aggregate targets are real.
  - Every package is mechanically consistent but adds churn without improving
    granularity for one-file packages.
- recommended_default: Convert both `internal/command` and `integration`, leave
  one-file packages alone.
- user_decision: Accepted recommended default on 2026-06-22 when the user asked
  to proceed to plan review and implementation after receiving Q-01.
- artifacts_updated:
  - plans/per-file-bazel-tests/00-requirements.md
  - plans/per-file-bazel-tests/10-implementation-plan.md
  - plans/per-file-bazel-tests/20-task-board.yaml
