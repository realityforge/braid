# Push Provenance Template Implementation Plan

Status: accepted
Date: 2026-06-21

## Phase Sequence

1. Planning review
   - Review `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, this
     plan, `20-task-board.yaml`, and `40-test-strategy.md`.
   - Apply review feedback before implementation.
   - Do not mark the plan accepted until the review outcome is recorded.
2. Git plumbing
   - Add `internal/gitexec` support for commit templates and structured history
     reads needed by provenance collection.
   - Preserve the existing `CommitVerbose` behavior when no template is present.
   - Ensure custom `core.commentChar` can be read from the source repo and
     copied/set in the temporary push repository.
3. Provenance discovery
   - Implement clean-anchor search over first-parent history.
   - Parse historical `.braids.json` with the existing config parser.
   - Compare historical downstream mirror tree items against the historical
     recorded upstream mirror item.
   - Stop at identity boundaries.
   - Collect eligible mirror commits after the anchor using full-history
     path traversal.
   - Re-check current mirror identity for every candidate commit before display
     so merged side branches cannot cross an identity boundary.
   - Filter Braid automatic commits and apply the fixed cap.
4. Template formatting and push integration
   - Format full commit messages as commented guidance.
   - Generate a temporary template only when eligible commits exist.
   - Pass the template to the temporary push repo's `git commit -v` while
     forcing cleanup that strips comment lines.
   - Warn and continue when provenance cannot be computed safely.
   - Reuse the shared push path so `braid sync` gets identical per-mirror
     guidance.
5. Documentation and validation
   - Update README pushing/sync documentation.
   - Add command tests for the accepted behavior matrix.
   - Run targeted gates while iterating and `bazel test //...` before marking
     implementation complete.

## Delivery Approach

- Keep Git invocations behind `internal/gitexec`.
- Keep provenance code focused and testable; avoid broad push refactors beyond
  the template hook needed by this feature.
- Preserve current push and sync success/failure behavior.
- Treat provenance as optional context: failures warn and continue.
- Use local temp repositories in tests; do not depend on global Git identity,
  the real Braid cache, or network remotes.
- Keep plan artifacts aligned with any review-driven scope changes.

## High-Risk Areas

- Comment character mismatch
  - Impact: commented guidance could become real upstream commit text.
  - Mitigation: read `core.commentChar`, copy/set single-character values in the
    temporary push repository, skip `auto`, and test custom comment characters.
- Commit cleanup mismatch
  - Impact: `commit.cleanup=whitespace` or similar config could preserve
    commented guidance in the upstream commit message.
  - Mitigation: force comment-stripping cleanup when a provenance template is
    used; add coverage for `commit.cleanup=whitespace`.
- Anchor search object availability
  - Impact: historical recorded revisions might be missing locally.
  - Mitigation: skip guidance with a warning rather than blocking push; test at
    least one missing-object or malformed-history warning path.
- Update-survival semantics
  - Impact: an intervening Braid update could accidentally truncate the local
    provenance window.
  - Mitigation: compare mirror content against each historical recorded
    revision to find actual clean states; add a test with local commit, remote
    update, `braid update`, then push.
- Merge-history semantics
  - Impact: first-parent-only or default simplified path history could omit
    feature branch commits or merge commits that touch the mirror.
  - Mitigation: first-parent only for anchor search, full-history path traversal
    for displayed commits; add required merge-history command coverage.
- Merged pre-boundary side branches
  - Impact: full-history traversal can include commits from a branch forked
    before a first-parent mirror identity change and merged afterward.
  - Mitigation: re-check current mirror identity for every displayed candidate
    commit; add a side-branch identity-boundary test.
- No-anchor terminal state
  - Impact: imported or historically dirty mirror history could leave
    implementers choosing inconsistent lower-bound behavior.
  - Mitigation: define root-with-matching-identity as all reachable path history
    for the current identity, with a commented no-anchor note and test coverage.
- Commit template integration
  - Impact: editor stdin behavior, `GIT_EDITOR`, or `git commit -v` output could
    change.
  - Mitigation: preserve existing no-template path and add tests that capture
    the editor file before overwriting it with the final upstream message.
- Conservative path-history listing
  - Impact: users may see reverted/transient downstream commits.
  - Mitigation: document guidance as "commits touching this mirror" rather than
    exact final-diff attribution.

## Required Full Gate

```bash
bazel test //...
```

## Targeted Gates

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
git diff --check
```

## Decision Outcomes

- All grill-me questions Q-01 through Q-16 are resolved.
- No behavior question remains open.
- The plan was accepted after user review, iterative plan-review feedback, and
  an explicit request to implement in a subagent.

## Decision Log

| id | Plan change |
| --- | --- |
| Q-01 | Provenance lists downstream commits that touched the mirror path, not exact final-diff line contributors. |
| Q-02 | Clean-anchor detection keeps local commits eligible across intervening Braid updates until the mirror is clean against its recorded upstream item. |
| Q-03 | No `.braids.json` metadata is added; provenance is derived at push time. |
| Q-04 | First-parent history finds the anchor; full reachable path history supplies displayed commits. |
| Q-05 | Local path, URL, and upstream path define identity; identity changes reset provenance. |
| Q-06 | Braid automatic add/update/remove commits are filtered by subject prefix. |
| Q-07 | Full commit messages are included, overriding the initial subject-only recommendation. |
| Q-08 | The provenance block is commented guidance, not default message text. |
| Q-09 | No generated upstream subject is added. |
| Q-10 | Configured single-character `core.commentChar` is respected; `auto` warns and skips guidance. |
| Q-11 | Displayed commits are capped at the newest 25 eligible commits, shown chronologically, with an older-omitted-count note. |
| Q-12 | The cap is fixed for the first implementation; no CLI flag is added. |
| Q-13 | Full messages are preserved line-for-line with every line commented. |
| Q-14 | Provenance failures warn and continue without blocking push. |
| Q-15 | `sync` receives the same per-mirror guidance through the shared push path. |
| Q-16 | Minimum coverage includes push guidance, update survival, Braid exclusion, comment-char handling, sync reuse, and cap behavior. |

## Plan Review Finding Log

| round | finding | assessment | plan change |
| --- | --- | --- | --- |
| R1 | Commit cleanup mode is unconstrained. | Valid. | Required templated commits to force comment-stripping cleanup and added `commit.cleanup=whitespace` coverage. |
| R1 | Merge-commit collection command contradicts requirements. | Valid. | Replaced default path-log wording with full-history path traversal and required merge-commit coverage. |
| R1 | Root/no-anchor terminal case is undefined. | Valid. | Defined root-with-matching-identity behavior as all reachable path history with a commented no-anchor note and coverage. |
| R1 | Cap selection is ambiguous. | Valid. | Specified retaining the newest 25 eligible commits and displaying them chronologically with an older-omitted-count note. |
| R1 | Task board weakens required edge-case coverage. | Valid. | Made both merge-history and identity-boundary coverage required acceptance criteria. |
| R2 | Full-history collection can cross identity boundaries through merged branches. | Valid. | Required per-candidate mirror identity filtering and side-branch identity-boundary coverage. |
| R3 | Cleanup integration coverage can false-pass. | Valid. | Required cleanup tests to make `commit.cleanup=whitespace` visible to the temporary push repository or assert the cleanup-forcing commit path directly. |

## Implementation Tasks

Tasks are tracked in `20-task-board.yaml`.

- `PLAN-APPROVAL`: review and accept or revise the plan.
- `T01`: add Git plumbing for templates, comment char, and history reads.
- `T02`: implement provenance discovery and formatting.
- `T03`: integrate guidance into push/sync.
- `T04`: add command coverage for edge cases and cap behavior.
- `T05`: update docs and run full validation.

## Completion Criteria

- User has reviewed and accepted the plan.
- All planned tasks in `20-task-board.yaml` are completed.
- No completed task has `commit.hash: pending`.
- README reflects final behavior and warning cases.
- Targeted gates pass during implementation.
- `bazel test //...` passes before completion.
- Task evidence is recorded.
- Working tree is clean or any exception is explicitly documented.
