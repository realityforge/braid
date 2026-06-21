# Push Provenance Template Test Strategy

Status: accepted
Date: 2026-06-21

## Required Gates

Full gate:

```bash
bazel test //...
```

Targeted gates:

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
git diff --check
```

## Gitexec Coverage

`internal/gitexec/gitexec_test.go`:

- Template-capable commit path invokes `git commit -v -t <template>` with
  comment-stripping cleanup and keeps the existing no-template `git commit -v`
  behavior available.
- Reading unset `core.commentChar` reports no configured value.
- Reading a single-character `core.commentChar` returns the exact character.
- `core.commentChar=auto` is observable to command code so guidance can be
  skipped.
- Any added log/blob/tree helper parses full commit messages, including bodies
  with blank lines.

## Command Unit Coverage

`internal/command/push_test.go`:

- Push editor receives a commented provenance block for downstream commits that
  touched the mirror path.
- Captured editor template includes full commit messages and full commit hashes.
- Final upstream commit message does not contain untouched commented guidance
  when the user writes their own message.
- Final upstream commit message does not contain untouched commented guidance
  when downstream config sets `commit.cleanup=whitespace`.
- The `commit.cleanup=whitespace` coverage must make that config visible to the
  isolated temporary push repository, for example with test-scoped
  `GIT_CONFIG_GLOBAL`/`HOME`, or directly assert that push invokes the
  cleanup-forcing templated commit path.
- Multiple downstream commits are shown in chronological order.
- Braid automatic add/update/remove commit subjects are excluded.
- A local downstream commit before an intervening `braid update` remains listed
  when local mirror changes still need pushing.
- Mirror identity change resets provenance so older commits for a previous URL
  or upstream path are not listed.
- A side branch forked before the current mirror identity, then merged after the
  first-parent identity boundary, has its mirror-path commits excluded.
- `core.commentChar` with a custom single character comments the template and
  is stripped by the final upstream commit.
- `core.commentChar=auto` prints a warning and opens the editor without
  provenance guidance.
- Provenance collection failure prints a warning and still allows push to
  succeed.
- Cap behavior displays the newest 25 eligible commits and an older-omitted-count
  note.
- A root-with-matching-identity history with no clean anchor produces the
  specified no-anchor guidance note.
- No eligible non-Braid commits opens the editor without provenance guidance and
  without a warning.
- Existing push cases remain covered: normal push, explicit branch, tag/revision
  explicit branch, editor stdin, editor failure, no local changes, not up to
  date, identity propagation, and sparse checkout filter behavior.

`internal/command/sync_test.go`:

- Default sync push receives the same commented provenance block for the pushed
  mirror.
- Multi-mirror sync opens one editor per pushed mirror and each editor receives
  only that mirror's provenance.
- `sync --pull-only` does not attempt provenance collection or open an editor.
- Existing sync push-plan, update, autostash, and cleanup tests remain covered.

## Focused Scenario Designs

### Update Survival

1. Create upstream with files `local.txt` and `remote.txt`.
2. Add branch mirror downstream.
3. Commit a local downstream change to `local.txt`.
4. Commit an upstream change to `remote.txt`.
5. Run `braid update <path>` so the mirror records the new upstream revision
   while retaining the local downstream change.
6. Run `braid push <path>` with an editor that captures the template then writes
   the final upstream message.
7. Assert the local downstream commit appears.
8. Assert the Braid update commit does not appear.

### Custom Comment Character

1. Configure downstream `core.commentChar` to `;`.
2. Push a mirror with eligible provenance.
3. Capture the editor template and assert guidance lines start with `; `.
4. Assert the final upstream commit message does not contain the commented
   guidance after the editor writes a real message.

### Cap Behavior

1. Create at least 26 downstream commits that touch the mirror path.
2. Push with a capturing editor.
3. Assert exactly 25 commit entries are present.
4. Assert the retained commits are the newest 25 eligible commits, displayed in
   chronological order.
5. Assert the omitted-count note names the older eligible commit count.

### Merge History

1. Create a local feature branch with a mirror-path commit.
2. Merge it into the downstream first-parent branch.
3. Include a merge commit whose resolution changes the mirror path.
4. Push with a capturing editor.
5. Assert the feature commit appears even if it is not on first-parent history.
6. Assert the merge commit that touched the mirror path appears.

### Side-Branch Identity Boundary

1. Create an old mirror identity and a side branch from that history.
2. On first-parent history, change the mirror identity for the same local path.
3. On the side branch, commit a mirror-path change under the old identity.
4. Merge the side branch after the identity change.
5. Push with a capturing editor.
6. Assert the old-identity side-branch commit is not listed.

### Root Without Clean Anchor

1. Create or mutate a downstream history where the mirror identity exists from
   root but never exactly matches its recorded upstream item.
2. Push with a capturing editor.
3. Assert the guidance includes the no-clean-anchor note.
4. Assert eligible commits from the current identity are still listed subject to
   the newest-25 cap.

## README Review Checks

- Pushing section explains the editor receives commented downstream provenance
  guidance when available.
- Sync section explains the same guidance appears per pushed mirror.
- Documentation states the user remains responsible for writing the upstream
  commit message.
- Documentation states the guidance is best-effort and skipped with a warning
  when it cannot be computed safely.
- Documentation states `core.commentChar=auto` skips guidance.

## Residual Risk To Review

- Historical recorded upstream objects may be unavailable in some repositories;
  the accepted behavior is warning and continuing without guidance.
- Path-limited commit history can include transient edits later reverted; the
  accepted behavior is conservative and documented.
- Subject-prefix filtering can exclude a user-authored commit that deliberately
  starts with a Braid automatic prefix.
