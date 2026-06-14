# Contributing

This repository is Bazel-first. Treat Bazel as the source of truth for builds,
tests, and Go toolchain selection.

## Build And Test

```bash
bazel test //...
bazel build //cmd/braid:braid
```

Use the rules_go toolchain for formatting:

```bash
bazel run @rules_go//go -- fmt internal/command/preflight_test.go
```

The required full gate is:

```bash
bazel test //...
bazel build --platforms=@rules_go//go/toolchain:linux_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:linux_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid
```

Release packaging and native smoke tests are documented in
[`docs/release.md`](release.md).

## Git Assumptions

- Runtime Git must be 2.43.0 or newer.
- Product code must call Git through `internal/gitexec` with explicit argument
  arrays. Do not introduce shell execution.
- Repository-mutating commands run only from the downstream worktree root in v1.
- Tests must not depend on the user's global Git identity, real Braid cache, or
  network remotes.
- Integration tests should create local upstream/downstream repositories in
  `t.TempDir()`, configure local user identity, and disable GPG signing unless
  the test explicitly covers signing config propagation.

## Test Strategy

Unit tests cover parsing, config validation, mirror naming, path validation,
cache precedence, and Git argv construction. Real-Git integration tests cover
command side effects, remotes, commits, tree/index operations, diffs, conflicts,
and push behavior.

Cross-platform confidence comes from two layers:

- Targeted tests for path separators, paths with spaces, shell metacharacters,
  argv preservation, and root-only execution.
- Bazel release-platform builds plus native smoke tests before a release cut.

Ruby oracle tests are migration aids only. Final gates are Go/Bazel-only.

## Plan Artifacts

The active implementation plan lives under `plans/go-port-braid/`. Keep
`20-task-board.yaml` current when executing planned work, including evidence and
commit metadata.
