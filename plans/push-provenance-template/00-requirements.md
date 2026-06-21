# Push Provenance Template Requirements

Status: accepted
Date: 2026-06-21

## Mission

Add best-effort, commented commit-message guidance to upstream commits created
by `braid push` and the push phase of `braid sync`.

When a mirror has committed downstream changes to push upstream, Braid should
help the user write the upstream commit message by showing the downstream
commits that touched the mirror path since the last clean mirror state. The
guidance must remain comments in Git's commit editor so the user can ignore,
summarize, or selectively copy it.

## Evidence From Existing Code

- `internal/command/push.go` computes the recorded upstream base revision,
  constructs the upstream `newTree` from downstream `HEAD:<mirror path>`, and
  opens Git's commit editor from an isolated temporary repository.
- `internal/command/sync.go` reuses `PushHandler.push` for each pushed mirror,
  so shared push-path integration can cover explicit `push` and `sync`.
- `internal/gitexec/gitexec.go` currently exposes `CommitVerbose`, which runs
  `git commit -v`; Git supports `git commit -t <template>` for editor
  templates.
- `copyLocalGitConfig` currently copies only identity and signing config into
  the temporary push repository, so any custom `core.commentChar` must be
  copied or explicitly set before commented guidance is safe.
- Product code that invokes Git must stay behind `internal/gitexec`.

## Scope

In scope:

- Generate provenance guidance only for actual upstream push commits.
- Cover `braid push <local_path>` and each pushed mirror in `braid sync`.
- Derive provenance from Git history at push time without adding fields to
  `.braids.json`.
- Identify downstream commits that touched the mirror path, not only commits
  that contributed surviving final-diff lines.
- Keep provenance across Braid updates while local mirror changes still exist.
- Reset provenance across mirror identity changes where local path, upstream
  URL, or upstream path changes.
- Exclude Braid automatic add/update/remove commits from the displayed list.
- Include full downstream commit messages.
- Format the provenance block as commented guidance using Git's effective
  comment character.
- Cap displayed commits at 25 and note omitted commits.
- Warn and continue when provenance cannot be computed safely.
- Update README and tests.

Out of scope:

- No new CLI flags or configuration fields.
- No blame-based final-line attribution.
- No persisted provenance metadata in `.braids.json`.
- No change to pushed tree construction or upstream freshness checks.
- No generated upstream commit subject.
- No behavior change for `sync --pull-only`.
- No attempt to include uncommitted mirror changes; push continues to use
  downstream `HEAD` only.

## Command Surface

No command syntax changes.

Affected commands:

```bash
braid push <local_path> [--branch|-b <branch>] [--keep]
braid sync [local_path...] [--autostash] [--keep]
```

Unaffected commands:

```bash
braid sync --pull-only [local_path...] [--autostash] [--keep]
braid add
braid update
braid diff
braid status
braid remove
```

## Behavior Requirements

1. Braid must only generate guidance when an upstream push commit will be
   attempted.
2. Existing early stops for no local changes and not-up-to-date mirrors remain
   unchanged.
3. Provenance collection must use downstream `HEAD` history, matching push's
   committed-content semantics.
4. The provenance window starts after the newest first-parent commit where the
   mirror content exactly matched that commit's recorded upstream mirror item.
5. The clean-anchor check must compare the downstream tree item at the mirror's
   local path with the upstream item recorded by `.braids.json` at that same
   downstream commit.
6. First-parent history is used only to find the clean anchor or identity
   boundary.
7. After the anchor is found, displayed commits come from full-history traversal
   in `anchor..HEAD` limited to the mirror path.
8. Merge commits that themselves touch the mirror path remain eligible.
9. Commits from merged branches that touched the mirror path remain eligible.
10. The provenance window must not cross a mirror identity boundary where the
    current local path is absent, the URL differs, or the upstream path differs.
11. Every displayed commit must independently have the current mirror identity in
    that commit's `.braids.json`; candidates without matching local path, URL,
    or upstream path are excluded even if they are reachable through a merge
    after the first-parent boundary.
12. Branch, tag, and revision changes alone do not reset provenance.
13. Braid automatic commits whose subject starts with `Braid: Add mirror `,
    `Braid: Update mirror `, or `Braid: Remove mirror ` are excluded.
14. The displayed block must include each selected commit hash and full commit
    message.
15. Commit messages must be preserved line-for-line in the guidance block.
16. Every guidance line must be prefixed by the effective Git comment character
    and one space.
17. If first-parent traversal reaches the root commit with matching mirror
    identity but no clean anchor, Braid must collect all eligible commits
    reachable from `HEAD` that touched the mirror path and add a commented note
    that no clean anchor was found.
18. Commits must be displayed in chronological order.
19. At most 25 eligible commits are displayed.
20. If more than 25 eligible commits exist, Braid must retain the newest 25
    eligible commits, display that retained set in chronological order, and add
    a commented note naming the count of older omitted commits.
21. No CLI flag is added to configure the cap.
22. If `core.commentChar` is unset, use `#`.
23. If `core.commentChar` is a single character, use that character and ensure
    the temporary push repository uses the same value for commit cleanup.
24. When a provenance template is used, the temporary push repository must force
    commit cleanup that strips comment lines regardless of the user's
    `commit.cleanup` configuration.
25. If `core.commentChar` is `auto`, skip provenance guidance and print a
    warning to stderr.
26. If provenance collection fails for missing objects, malformed historical
    config, unsupported comment-char configuration, or another non-push-critical
    reason, print a warning to stderr and continue the push without guidance.
27. Provenance warnings must not convert a valid push into a failure.
28. If no eligible non-Braid commits are found, Braid should open the editor
    without a provenance template and without a warning.
29. `sync` must receive the same per-mirror guidance by reusing the shared push
    path.
30. The temporary push repository and the user's worktree/index must remain
    untouched except for the existing push behavior.

## Acceptance Criteria

- [ ] `braid push` opens the editor with commented full-message provenance for
      downstream commits touching the selected mirror.
- [ ] Leaving commented guidance in the editor does not include it in the
      upstream commit message, even when the user has configured
      `commit.cleanup=whitespace`.
- [ ] Custom single-character `core.commentChar` is respected and copied into
      the temporary push repository.
- [ ] `core.commentChar=auto` warns and skips provenance guidance.
- [ ] Braid update commits are excluded, while earlier local downstream commits
      remain listed after an intervening `braid update` when local changes still
      need pushing.
- [ ] Full-history path traversal is used after the first-parent anchor, so
      merged-branch commits and merge commits that touch the mirror can be
      listed.
- [ ] Mirror identity changes reset the provenance window.
- [ ] Side-branch commits whose historical config predates the current mirror
      identity are excluded even when merged after the first-parent identity
      boundary.
- [ ] Reaching root with matching identity and no clean anchor has explicit,
      tested behavior.
- [ ] Cap behavior displays the newest 25 eligible commits in chronological
      order and adds an older-omitted-count note.
- [ ] Provenance failures warn and continue without blocking the push.
- [ ] `braid sync` shows equivalent per-mirror guidance for each pushed mirror.
- [ ] README documents the guidance behavior and warning cases.
- [ ] Targeted command tests and `bazel test //...` pass.

## Quality Gates

Required full gate:

```bash
bazel test //...
```

Targeted gates:

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
git diff --check
```

## Known Intentional Divergences

- Provenance is conservative: commits that touched the mirror path but whose
  changes were later reverted may appear.
- Provenance is not line-level attribution and does not use blame.
- Guidance is best-effort and non-blocking; push correctness remains governed
  by existing tree comparison, upstream freshness, editor, and push checks.
- A user-authored commit with a subject that starts with a Braid automatic
  commit prefix is excluded.
- `core.commentChar=auto` skips guidance instead of guessing which comment
  character Git will choose.

## Open Questions Register

All grill-me questions are resolved; no implementation-blocking questions
remain. The plan was reviewed through the iterative plan-review loop and
accepted for implementation after the user requested subagent implementation.

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | Should the template list all downstream commits that touched the mirror path or only final-diff contributors? | Blame misses deletions and binary changes; path history can include transient edits. | All touching commits; final-diff contributors. | All touching commits is complete and honest but conservative. | List all downstream commits touching the mirror path. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-02 | resolved | Should commits before an intervening `braid update` remain eligible when local changes still need pushing? | Update can merge new upstream content while preserving local mirror edits. | Keep provenance alive; reset on update commit. | Keeping provenance avoids dropping local commits that remain part of the pushed tree. | Keep provenance until the mirror is clean against the recorded upstream revision. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-03 | resolved | Should Braid persist provenance state or derive it from history? | New metadata would require migration and could become stale. | Persist state; derive from Git history. | History derivation is slower but works for existing repos without config churn. | Derive at push time. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-04 | resolved | How should merge-heavy history be handled? | First-parent path history can hide feature commits behind merge commits. | First-parent only; full history only; first-parent anchor plus full history list. | The mixed approach anchors to integration history while crediting actual mirror edits. | First-parent for anchor, full reachable path history for displayed commits. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-05 | resolved | Should provenance cross mirror identity changes? | Same local path can be reused for a different URL or upstream path. | Cross identity changes; reset at identity boundary. | Resetting avoids attributing changes from a different upstream identity. | Reset when local path, URL, or upstream path identity changes. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-06 | resolved | Should Braid automatic commits be included? | Add/update/remove commits can touch mirror paths but are bookkeeping. | Include all; exclude Braid automatic subjects. | Exclusion focuses on downstream-authored context with a small prefix-collision tradeoff. | Exclude `Braid: Add/Update/Remove mirror ...` subjects. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-07 | resolved | Should the guidance include subjects only or full commit messages? | Subjects are concise; bodies can carry important rationale. | Subjects only; full messages. | Full messages are noisier but preserve context for the user to summarize. | Subjects only. | User chose full commit messages. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-08 | resolved | Should provenance be real default message text or commented guidance? | Non-comment template text is committed if left in place. | Actual message text; commented guidance. | Comments avoid accidental long upstream messages and keep user control. | Actual message text. | User chose commented template guidance. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-09 | resolved | Should Braid generate a non-comment default upstream subject? | Git aborts unchanged comment-only templates, preserving current user-authored-message flow. | Generate subject; comment-only guidance. | Comment-only guidance avoids inventing an upstream message. | Comment-only guidance. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-10 | resolved | How should configurable Git comment characters be handled? | Hardcoded `#` becomes real text when `core.commentChar` differs. | Hardcode `#`; use configured single char; guess for auto. | Respecting config prevents accidental message contamination; `auto` is unsafe to guess. | Use configured single char or `#` when unset; skip with warning for `auto`. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-11 | resolved | Should the block include every matching commit or be capped? | Full messages over long-lived vendor branches can make editors unwieldy. | Unlimited; fixed cap; configurable cap. | A fixed cap keeps the first implementation usable without new interface. | Cap at 25 commits with an omitted-count note. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-12 | resolved | Should the cap be configurable now? | No evidence yet that a user-facing knob is needed. | Add flag; fixed cap. | Fixed cap avoids interface churn. | No new CLI flag; fixed cap of 25. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-13 | resolved | How should full commit messages be formatted? | Bodies can contain blank lines, trailers, and comment-like content. | Normalize text; preserve line-for-line and comment every line. | Preservation keeps source messages faithful while comments keep the template safe. | Preserve line-for-line and prefix every line with comment character plus space. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-14 | resolved | Should provenance failures block push? | The pushed tree remains valid even if guidance cannot be computed. | Fail push; warn and continue. | Non-blocking warnings preserve push reliability. | Warn and continue. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-15 | resolved | Should `sync` include the same guidance for each pushed mirror? | `sync` invokes the existing push path once per pushed mirror. | Push only; push and sync. | Shared behavior avoids inconsistent upstream commit editor UX. | Include guidance per pushed mirror through the shared push path. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-16 | resolved | What is the minimum test coverage? | The feature touches Git history traversal, commit templates, comment cleanup, and sync reuse. | Light smoke tests; focused command tests. | Focused command tests prove behavior with real local Git repositories. | Cover push guidance, update survival, Braid commit exclusion, comment char handling, sync reuse, and cap behavior. | Accepted recommendation. | `00-requirements.md`, `01-provenance-algorithm-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
