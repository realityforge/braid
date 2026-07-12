# Multi-Mirror Source Implementation Plan

## Phase sequence

1. Perform a repo-wide mechanical cut to `internal/source`, explicit lookup APIs, and new v2 fixtures so every production consumer compiles without compatibility fields; implement strict config and deterministic v1 upgrade in the same slice.
2. Implement explicit source/name/path lookup, selector resolution, variadic add modes, path-scoped removal, and completion.
3. Replace single-path pull with aggregate source-tree merge; adapt status and diff to path/source selection and missing paths.
4. Implement overlap-aware upstream reconstruction; adapt push, sync, cache identity/hydration, message generation, and per-mirror provenance.
5. Complete docs and integration coverage, run all CI-parity gates, and perform iterative implementation-alignment review.

## Delivery rules

- Remove the rejected compatibility model rather than retaining shims.
- Keep one task active and run targeted tests after each behavior slice.
- Use explicit domain types at command boundaries; do not pass raw strings where source/mirror identity matters.
- Preserve unrelated worktree/index state and existing Git-isolation test rules.

## High-risk areas and mitigation

- Aggregate conflict/index behavior: construct old/new downstream trees from HEAD and exercise clean, local-only, remote-only, conflict, deletion, and file/directory transitions.
- Upstream overlap reconstruction: normalize into an upstream path trie, validate shared tree items before applying outermost entries, and test duplicate/root/nested/deletion cases.
- Broad fixture migration: make schema failures explicit and search for `Mirror.Path`, `RemotePath`, path-keyed `Mirrors`, and old JSON keys after each phase.
- Partial-clone/cache sharing: separate source object identity from hydration requirements and test two sources sharing a URL.
- Provenance drift: test unchanged, added, removed, and remapped mirrors and enforce the v1 boundary.
- Push freshness: resolve the requested destination and use compare-and-swap semantics, including alternate and unborn branches.

## Required gates

- `bazel run @rules_go//go -- fmt ./...`
- `git diff --exit-code`
- `bazel test --test_env=PATH //...`
- `bazel run @rules_go//go -- vet ./...`
- `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
- `bazel test --test_env=PATH //integration/...`

Also run plans-empty after cleanup, `git --version`, and the workflow's `git merge-tree --write-tree` capability probe exactly as CI does.

## Delivery

- One cohesive PR with reviewable commits where practical.
- Commit implementation and review fixes before deleting this plan tree.
- Remove this plan tree in a deletion-only cleanup commit because CI requires `plans/` empty.
- Push the branch, create a ready PR, assign the authenticated GitHub user, and enable auto-merge.
- Verify the PR head/base, assignee, auto-merge state, required checks, and aggregate `CI success` after publication.
