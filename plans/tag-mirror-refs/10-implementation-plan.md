# Tag Mirror Ref Isolation Implementation Plan

## Phase Sequence

1. Add regression assertions that tag add/update paths do not populate the
   downstream global tag namespace, do not change a pre-existing same-named
   downstream tag, and retain isolated tracking refs only when requested.
2. Change tag `LocalRef` and downstream tag fetch refspecs to use
   `refs/remotes/<braid-remote>/tags/<tag>` consistently, including the
   repository-local-cache fetch path.
3. Add the PR-only delivery non-negotiable to `AGENTS.md` and update user-facing
   documentation only if the implementation exposes a contract worth stating.
4. Run targeted tests followed by every CI-parity check, inspect the diff,
   commit, push, and open a ready pull request.

## Expected Files

- `internal/mirror/mirror.go`
- `internal/mirror/mirror_test.go`
- `internal/command/cache.go`
- focused command or integration tests for tag ref behavior
- `AGENTS.md`

## High-Risk Areas

- Annotated tag peeling:
  - Impact: resolving the tracking ref incorrectly could import a tag object
    rather than its commit or fail tree extraction.
  - Mitigation: retain `^{commit}` resolution and test annotated tags.
- Cache-mode divergence:
  - Impact: repository-local cache has an explicit downstream refspec distinct
    from the normal/global-cache path.
  - Mitigation: assert behavior across no-cache, global-cache, and
    repository-local-cache modes.
- Accidental deletion of user refs:
  - Impact: fetching or cleanup could overwrite or delete a user-owned global
    tag.
  - Mitigation: test a pre-existing same-named tag by object ID; never target
    `refs/tags/<tag>` in downstream fetch or cleanup operations.

## Required Full Gates

1. `bazel run @rules_go//go -- fmt ./...`
2. `git diff --exit-code` immediately after formatting on the committed
   implementation, proving formatting introduced no changes
3. `bazel test --test_env=PATH //...`
4. `bazel run @rules_go//go -- vet ./...`
5. `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
6. `bazel test --test_env=PATH //integration/...`

## Completion Criteria

- All acceptance criteria and tasks are complete with recorded evidence.
- All CI-parity gates pass.
- Final `git diff` and `git status` are reviewed after any plan-state-only
  follow-up commit.
- The diff contains only the approved change, tests, plan state, and requested
  repository instruction.
- Changes are committed, pushed, and available in a ready pull request.
