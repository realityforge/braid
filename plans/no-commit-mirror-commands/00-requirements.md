# No-Commit Mirror Commands Requirements

Status: accepted

## Mission

Allow `add`, `pull`/`update`/`up`, and `remove` to stage Braid-owned mirror changes without creating an automatic commit, so users can include mirror changes in the same commit as other related work.

## Scope Boundaries

- In scope: `--no-commit` for `add`, canonical `pull` plus `update`/`up` aliases, and `remove`.
- In scope: staged changes for `.braids.json` and selected mirror paths only.
- In scope: preserving unrelated staged, unstaged, untracked, and ignored files outside command-owned paths.
- In scope: unit tests covering all behavior and integration tests covering multiple real executable paths.
- Out of scope: `sync --no-commit`.
- Out of scope: supporting dirty command-owned paths.
- Out of scope: broad rollback on staging failure.
- Out of scope: no-`HEAD` repository support beyond current behavior.

## Locked Decisions And Non-Negotiables

- `--no-commit` means stage Braid changes without committing.
- `HEAD` must remain unchanged for all successful non-conflict no-commit operations.
- Command-owned paths must be clean before the operation.
- Unrelated staged changes warn but do not block.
- Success confirmations are suppressed by `--quiet`; warnings still print.
- Conflict behavior for `pull --no-commit` is the same as current pull conflict behavior.
- Synthetic final-tree computation should remain the source of truth; no-commit mode materializes selected paths from that tree into the real index and worktree.
- Unit tests must cover all functionality; integration tests must cover more than one happy path.
- No-path `pull --no-commit` intentionally allows Git-style partial progress after the initial all-target preflight: each successful mirror may be staged before a later mirror fails or conflicts.

## Command Surface And Behavior Expectations

### Add

- `braid add <url> [local_path] ... --no-commit` creates or updates `.braids.json`, materializes mirror content, stages `.braids.json` and the mirror path, and creates no commit.
- Missing `.braids.json` remains valid for first add.
- Existing target and dirty config/mirror target checks remain enforced.
- Temporary add remote cleanup remains unchanged.

### Pull / Update / Up

- `braid pull [local_path] ... --no-commit` stages the same mirror/config delta that the automatic Braid commit would have contained.
- `update` and `up` inherit this behavior.
- No-op pull leaves index/worktree untouched and exits successfully.
- Strategy changes that affect config stage `.braids.json` even when mirror content is identical.
- No-path `pull --no-commit` updates every eligible branch/tag mirror, skips revision-locked mirrors as today, and preflights all eligible targets before side effects.
- No-path `pull --no-commit` must not rerun command-owned cleanliness checks in a way that rejects `.braids.json` or mirror paths staged earlier by the same invocation.
- No-path `pull --no-commit` unrelated-staged warning detection must treat `.braids.json` plus every eligible mirror path selected by that invocation as command-owned, so earlier same-invocation staged mirror paths do not trigger a false unrelated-staged warning.
- Conflict behavior remains current behavior: conflict markers in mirror worktree, staged `.braids.json`, `.git/MERGE_MSG`, and printed resolution commands.

### Remove

- `braid remove <local_path> --no-commit` stages deletion of mirror content and the `.braids.json` entry, and creates no commit.
- `--keep` continues to mean keep the Braid remote; it does not keep mirror files.
- Remote cleanup error wording should mirror existing post-commit wording with "staged" instead of "committed".

## Quality / Test / Coverage Gates

Targeted checks during implementation:

- `bazel test //internal/cli:cli_test`
- `bazel test //internal/command/...`
- `bazel test //internal/command:add_test //internal/command:refresh_command_test //internal/command:remove_test //internal/command:completion_test`
- relevant integration targets, especially `//integration:scoped_state_test`, `//integration:lifecycle_test`, `//integration:conflict_test`, and any new no-commit integration target if added

Required full gate before claiming ready:

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test --test_env=PATH //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
bazel test --test_env=PATH //integration/...
```

CI workflow note: `.github/workflows/ci.yml` also runs `git --version`, a `git merge-tree --write-tree` smoke check, and uses `--test_env=PATH` for Bazel test steps.

Exact CI-parity test commands in the required gate must use `--test_env=PATH`.

## Known Intentional Divergences

- No new `sync --no-commit` support.
- Ignored files inside selected mirror paths keep current command behavior and do not become a new explicit blocker unless Git refuses the operation.
- No success `MERGE_MSG` is written in non-conflict no-commit mode.
- No broad rollback is promised if staging from the synthetic tree fails.

## Output Contract

- Success confirmations are written to stdout unless `--quiet` is set.
- Success confirmations use these forms:
  - `Braid: staged add of mirror '<path>'`
  - `Braid: staged update of mirror '<path>'`
  - `Braid: staged removal of mirror '<path>'`
- Pre-existing unrelated staged changes are detected before staging Braid paths and warn on stdout before any success confirmation:
  - `Braid: warning: unrelated staged changes are present; unstage them before committing if they should not be included.`
- For no-path `pull --no-commit`, "unrelated" excludes every eligible mirror path selected by that invocation, not just the mirror currently being processed.
- `pull --no-commit` no-op writes no stdout.
- No-path `pull --no-commit` keeps current skipped locked mirror stdout after processing selected mirrors.
- Cleanup failures are command errors and flow through the existing app error path on stderr, with staged-specific wording where the Braid paths were already staged.
- Progress remains on stderr and remains controlled by existing quiet behavior.
- Root completion continues to expose only canonical commands. After a user has typed `update` or `up`, command-specific option and mirror-path completion should work for the alias, including `--no-commit`.

## Open Questions Register

All product/design questions raised during grill review are resolved. Plan approval remains tracked as task `PLAN-APPROVAL` in `20-task-board.yaml`, not as an open product question.

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | Should canonical `pull` expose `--no-commit` as well as aliases? | `update` and `up` parse as `pull`; docs prefer `pull`. | Pull plus aliases; aliases only. | Alias-only creates hidden split. | Add to `pull`, inherited by aliases. | Accepted recommendation. | 00, 10, 20 |
| Q-02 | resolved | Should no-commit stage only Braid-owned paths? | Existing code isolates automatic commits to command paths. | Braid-owned only; include outside related changes. | Outside inclusion is unknowable and risky. | Stage `.braids.json` plus selected mirror paths only. | Accepted recommendation. | 00, 10, 20 |
| Q-03 | resolved | Should dirty `.braids.json` or selected mirror paths be allowed? | Command rewrites those paths. | Allow; reject. | Allowing needs same-path merge preservation. | Reject dirty command-owned paths. | Accepted recommendation. | 00, 10, 20 |
| Q-04 | resolved | What should no-op pull do? | Current pull exits without a commit when already current. | Touch nothing; stage empty state. | Empty staging is noise. | Leave untouched and succeed. | Accepted recommendation. | 00, 10, 20 |
| Q-05 | resolved | Should pull conflict behavior differ? | Current conflict path already avoids automatic commit. | Same behavior; special no-commit flow. | Special flow adds noise without changing recovery. | Keep current conflict behavior. | Accepted recommendation. | 00, 10, 20 |
| Q-06 | resolved | Support no-path `pull --no-commit`? | Current no-path pull updates eligible mirrors. | Support; reject. | Support enables one pending commit for multiple mirrors. | Support with all-target preflight. | Accepted recommendation. | 00, 10, 20 |
| Q-07 | resolved | Write `MERGE_MSG` on non-conflict success? | User wants combined manual commit. | Write; do not write. | Writing state is unnecessary and stale-prone. | Do not write unless conflict. | Accepted recommendation. | 00, 10, 20 |
| Q-08 | resolved | Print success message after staging? | Auto-commit paths are mostly quiet, but staging needs confirmation. | Print; stay silent. | Message confirms no commit was made. | Print minimal stdout success unless quiet. | Accepted recommendation. | 00, 10, 20 |
| Q-09 | resolved | Should no-commit run hooks? | No commit is created. | Emulate hooks; run none. | Hook emulation is surprising. | No hooks run. | Accepted recommendation. | 00, 10, 20 |
| Q-10 | resolved | Change remote cleanup behavior? | Current add/pull/remove clean temporary or Braid remotes. | Change; keep. | Staging should not leave extra remotes. | Keep existing cleanup semantics. | Accepted recommendation. | 00, 10, 20 |
| Q-11 | resolved | Can first `add --no-commit` create `.braids.json`? | Current add can create config. | Yes; no. | Rejecting blocks common first-add workflow. | Yes. | Accepted recommendation. | 00, 10, 20 |
| Q-12 | resolved | Does `remove --keep --no-commit` keep files? | Current `--keep` means keep remote only. | Keep files too; keep remote only. | Overloading changes existing meaning. | Keep remote only. | Accepted recommendation. | 00, 10, 20 |
| Q-13 | resolved | Warn on unrelated staged changes? | Next manual commit includes all staged files. | Warn; block; ignore. | Blocking prevents intended combined commits; ignoring hides risk. | Warn but allow. | Accepted recommendation. | 00, 10, 20 |
| Q-14 | resolved | Document as staged or unstaged? | Proposal explicitly asked for staging. | Stage; leave unstaged. | Staged state is directly ready for commit. | Document staged changes. | Accepted recommendation. | 00, 10, 20 |
| Q-15 | resolved | Use another flag name? | `--no-commit` is Git-flavored. | `--no-commit`; `--staged`; `--no-auto-commit`. | Longer names reduce ambiguity but add friction. | Keep `--no-commit`. | Accepted recommendation. | 00, 10, 20 |
| Q-16 | resolved | Add `sync --no-commit`? | Sync push planning uses committed state. | Add; defer. | Adds push/pull complexity. | Do not add to sync. | Accepted recommendation. | 00, 10, 20 |
| Q-17 | resolved | Reject unrelated staged changes? | User wants same final commit with related work. | Reject; warn. | Rejection forces awkward unstaging. | Warn but allow. | Accepted recommendation. | 00, 10, 20 |
| Q-18 | resolved | Should command-owned paths be clean after staging? | Stage changes without committing should not leave extra unstaged command diffs. | Clean; leave unstaged. | Clean state avoids extra `git add`. | Apply to index and worktree. | Accepted recommendation. | 00, 10, 20 |
| Q-19 | resolved | Reuse synthetic final tree for no-commit? | Existing code already computes synthetic commit trees. | Reuse; rewrite direct file operations. | Reuse preserves existing correctness. | Restore selected paths from final tree. | Accepted recommendation. | 00, 10, 20 |
| Q-20 | resolved | Use synthetic tree for remove deletions? | Remove already computes tree without mirror path. | Synthetic restore; manual delete and add. | Synthetic path keeps behavior consistent. | Restore from tree without mirror. | Accepted recommendation. | 00, 10, 20 |
| Q-21 | resolved | Avoid touching real config before staging? | Update currently writes config for hashing. | Avoid where possible; write first. | Avoiding reduces transient dirty state. | Use `HashBytes` in success paths where clean. | Accepted recommendation. | 00, 10, 20 |
| Q-22 | resolved | Adjust non-no-commit pull config hashing? | Same latent cleanup risk in existing pull success path. | Change if clean; leave. | Scoped cleanup improves safety without broad refactor. | Use `HashBytes` where it falls out cleanly. | Accepted recommendation. | 00, 10, 20 |
| Q-23 | resolved | Update usage/completion/tests/docs? | Flag is user-facing. | Yes; defer. | Deferral creates drift. | Update all in same change. | Accepted recommendation. | 00, 10, 20 |
| Q-24 | resolved | What minimum tests are required? | Initial proposal listed core coverage. | Minimum only; broader. | Minimum misses regression risk. | Cover parser, behavior, no-op, all-pull, remove, warnings, dirty blockers. | User required more, especially integration. | 00, 10, 20, 40 |
| Q-25 | resolved | Should integration tests be primary proof? | User clarified unit tests must cover all functionality. | Integration primary; unit comprehensive. | Handler-level tests catch more edge paths. | Unit tests cover all behavior; integration tests cover multiple real paths. | User clarified and accepted refined direction. | 00, 10, 20, 40 |
| Q-26 | resolved | Should ignored files block? | Current scoped status ignores ignored files. | Block; keep current. | Blocking changes existing command behavior. | Keep current behavior. | Accepted recommendation. | 00, 10, 20 |
| Q-27 | resolved | Should quiet suppress staged confirmation? | Quiet suppresses progress/noise. | Suppress; print anyway. | Warnings still need visibility. | Suppress success, keep warnings. | Accepted recommendation. | 00, 10, 20 |
| Q-28 | resolved | Stage deletions from upstream changes? | Final tree may remove mirror files. | Stage exact tree; skip deletions. | Skipping deletions makes cached diff wrong. | Stage additions, modifications, deletions, renames as final tree. | Accepted recommendation. | 00, 10, 20, 40 |
| Q-29 | resolved | Stage config-only tracking changes? | Strategy changes may not alter content. | Stage config; no-op. | No-op loses requested metadata change. | Stage `.braids.json`. | Accepted recommendation. | 00, 10, 20 |
| Q-30 | resolved | Must `HEAD` remain unchanged? | Core flag purpose. | Yes; maybe conflict exception. | Any commit violates feature. | `HEAD` unchanged in success and conflict states. | Accepted recommendation. | 00, 10, 20, 40 |
| Q-31 | resolved | Cleanup failure after staging behavior? | Main requested state may already be applied. | Error and leave staged; rollback. | Rollback risks unrelated work. | Error with staged state left in place. | Accepted recommendation. | 00, 10, 20 |
| Q-32 | resolved | Cleanup failure wording? | Existing post-commit messages say committed. | Mirror wording; invent new. | Parallel wording is clear. | Replace committed with staged. | Accepted recommendation. | 00, 10, 20 |
| Q-33 | resolved | Roll back staging failure? | Reliable rollback is hard with unrelated state. | Rollback; no broad rollback. | Rollback can disturb user work. | Return error and leave Git state. | Accepted recommendation. | 00, 10, 20 |
| Q-34 | resolved | Support repositories without initial commit? | Current implementation assumes `HEAD`. | Add support; keep current. | New support expands scope. | No special support. | Accepted recommendation. | 00, 10, 20 |
| Q-35 | resolved | Document already-staged footgun? | Next commit includes all staged files. | Document; omit. | Omission hides a likely mistake. | Document explicitly. | Accepted recommendation. | 00, 10, 20 |
| Q-36 | resolved | Add shared helper for no-commit staging/warnings? | Behavior crosses add/pull/remove. | Shared helper; duplicate. | Helper prevents inconsistent behavior. | Add small internal command helper. | Accepted recommendation. | 00, 10, 20 |
| Q-37 | resolved | Warn before or after staging? | Warning should describe pre-existing index. | Before; after. | After may count Braid paths. | Warn before staging. | Accepted recommendation. | 00, 10, 20 |
| Q-38 | resolved | Mention no-commit in conflict output? | Conflict next actions do not change. | Mention; keep same. | Extra note adds noise. | Keep same conflict output. | Accepted recommendation. | 00, 10, 20 |
| Q-39 | resolved | Stop grilling and implement? | Product decisions are settled. | Stop; continue. | More questions are lower-value. | Move to implementation planning. | User requested structured plan output first. | 00, 10, 20 |
| R1-01 | resolved | How should no-path `pull --no-commit` handle state staged earlier in the same invocation? | Review found current sequential `updateOne` would see `.braids.json` as dirty after the first staged mirror if not specified. | Aggregate once; explicitly allow sequential partial staging. | Aggregation is cleaner but larger; sequential partial staging matches earlier accepted partial-progress behavior. | Sequential partial staging with one upfront all-target preflight and no later self-dirty rejection. | Applied during iterative plan review. | 00, 10, 20, 40 |
| R1-02 | resolved | What exact targeted command should replace nonexistent `//internal/command:command_test`? | Review found no such Bazel target in `internal/command/BUILD.bazel`. | Use actual per-file targets; use package wildcard. | Wildcard is simple and cannot go stale as easily. | Use `bazel test //internal/command/...` plus focused explicit targets when useful. | Applied during iterative plan review. | 00, 20, 40 |
| R1-03 | resolved | Should required gate include CI `--test_env=PATH`? | CI workflow uses it for both test steps. | Include; omit and note. | Omitting weakens CI parity. | Include exact CI test commands. | Applied during iterative plan review. | 00, 10, 20, 40 |
| R1-04 | resolved | How is integration coverage made enforceable? | Review found scenarios only in test strategy, not task board. | Add task; rely on final gate. | Dedicated task makes coverage auditable. | Add `INT-001` with scenario acceptance criteria. | Applied during iterative plan review. | 10, 20, 40 |
| R1-05 | resolved | Should completion include aliases? | Existing root completion intentionally hides aliases. | Canonical only; alias-aware after typed alias. | Alias-aware completion helps users without exposing aliases as preferred root commands. | Keep root canonical-only; support option/path completion after typed `update` or `up`. | Applied during iterative plan review. | 00, 10, 20, 40 |
| R1-06 | resolved | What is the no-commit output stream/message contract? | Review found stdout/stderr and ordering ambiguous. | Specify exact text/streams; leave to implementer. | Exact text prevents test/doc drift. | Specify success, warning, no-op, skipped, cleanup, and progress behavior. | Applied during iterative plan review. | 00, 10, 20, 40 |
| R2-01 | resolved | Should no-path pull warning detection exclude all selected mirror paths? | Review found earlier same-invocation staged mirrors could be falsely reported as unrelated. | Exclude current mirror only; exclude invocation-selected mirror set. | Current-only exclusion creates false warnings in sequential no-path staging. | Exclude `.braids.json` plus every eligible mirror path selected by the invocation. | Applied during iterative plan review. | 00, 10, 20, 40 |
