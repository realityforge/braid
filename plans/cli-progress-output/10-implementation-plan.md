# CLI Progress Output Implementation Plan

Status: accepted

## Delivery Approach

Implement the feature in small behavior slices: parse quiet first, add a reusable progress reporter, integrate command-level progress in selected workflows, then update documentation and run CI parity gates.

The plan has been reviewed and accepted for implementation.

## Phase Sequence

1. Resolve output-contract questions.
2. Add `cli.GlobalOptions.Quiet`, parser support, usage text, and quiet/verbose conflict tests.
3. Add a small command-layer progress helper that writes semantic messages to `stderr`, can be disabled by quiet, and supports TTY-aware dot ticking.
4. Integrate progress around all operations listed in `30-output-contract.md`, including data commands while preserving their `stdout` data.
5. Update README and migration docs to document the new output contract.
6. Run targeted tests during implementation and full CI parity before completion.

## High-Risk Areas And Mitigations

- stdout/stderr regression: keep data output on `stdout`; assert progress appears on `stderr`.
- data-command noise: add tests proving `status` and `diff` retain their requested `stdout` data and put progress only on `stderr`.
- recovery stream churn: preserve existing non-progress recovery/result streams unless an output is explicitly reclassified as progress.
- test drift from contract: map targeted tests directly to each operation row and stream class in `30-output-contract.md`.
- verbose interaction: keep raw Git tracing in `internal/gitexec`; normal progress should not duplicate argv traces.
- quiet overreach: suppress only progress/info; do not suppress warnings, errors, or recovery instructions.
- multi-mirror noise: prefer one start/completed pair per selected mirror or remote phase only where useful.
- message precision: use concise semantic operation verbs rather than command-level banners so multi-phase commands do not imply whole-command completion too early.
- no-op noise: emit completed messages for no-op outcomes only after remote work occurred; avoid adding output to purely local no-op paths.
- terminal behavior: isolate dot ticking behind a testable writer/terminal probe and fake ticker/clock rather than ad hoc timers in handlers.
- Bazel metadata: update explicit `internal/command/BUILD.bazel` source/test lists for any new helper files; avoid new external dependencies unless stdlib terminal detection proves insufficient.
- CI platform matrix: local checks mirror workflow commands, but final reporting must state whether the GitHub Actions matrix itself was observed or remains a platform validation gap.

## Required Full Gate

1. `bazel run @rules_go//go -- fmt ./...`
2. `git diff --exit-code`
3. `git --version`
4. `git merge-tree --write-tree "--merge-base=HEAD^{tree}" "HEAD^{tree}" "HEAD^{tree}"`
5. `bazel test --test_env=PATH //...`
6. `bazel run @rules_go//go -- vet ./...`
7. `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
8. `bazel test --test_env=PATH //integration/...`

## Decision Log

- Accepted: `--quiet` is global and incompatible with `--verbose`; reflected in parser and usage tasks.
- Accepted: progress/info uses `stderr`, command data remains on `stdout`; reflected in progress helper and command integration tasks.
- Accepted: dot ticking is TTY-aware and newline-safe; reflected in progress helper tests.
- Accepted: default progress avoids raw upstream URLs; reflected in message wording tasks.
- Q-01: User selected B. Progress should cover every command path that may contact a remote or cache, including `add`, `pull`, `push`, `sync`, `status`, `diff`, and setup/cache hydration paths. Plan updated to include data-command stdout/stderr tests and broader command integration.
- Q-02: User selected A. Progress messages should use concise semantic operation verbs such as `fetching mirror`, `fetched mirror`, `pushing mirror`, `pushed mirror`, and `updated mirror`.
- Q-03: User selected B. Long-running TTY operation dots should tick every 5 seconds.
- Q-04: User selected A. `--quiet` suppresses progress/info only; warnings remain visible.
- Q-05: User selected A. Print start/completed only when remote work was performed; completed text may say `already up to date`.
- Review round 1: Added `30-output-contract.md` to define exact operation coverage, stream classes, setup behavior, reporter lifecycle, terminal probe strategy, and CI matrix caveat.
- Review round 2: Split pull/sync revision checking from content update so no-op completion is not tied to a misleading `updating mirror` start; tightened tests to cover every output-contract row and stream class.
- Approval: User approved implementation after iterative plan review returned no findings.

## Task Breakdown

- PLAN-01: Create and maintain planning artifacts until plan acceptance.
- T-01: Add global quiet parsing and usage behavior.
- T-02: Add progress reporter helper with quiet, TTY-aware ticking, and failure cleanup behavior.
- T-03: Integrate command-level progress in all operation-matrix workflows.
- T-04: Update docs and migration notes.
- T-05: Run full CI parity and clean up.
- PLAN-APPROVAL: Request user review and record acceptance before implementation starts.
