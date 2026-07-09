# No-Commit Mirror Commands Test Strategy

Status: accepted

## Test Principle

Unit tests must cover all functional behavior. Integration tests must cover multiple executable workflows, including similar paths to unit tests, so the feature is proven through real Git process behavior and not only handler seams.

## Unit Coverage

### CLI and completion

- Parse `--no-commit` for `add`, `pull`, `update`, `up`, and `remove`.
- Reject `--no-commit` for unsupported commands.
- Usage strings include `--no-commit` on canonical command help.
- Root completion remains canonical-only.
- Completion includes `--no-commit` and mirror-path candidates for canonical `pull` and for typed `update`/`up` aliases.

### Shared no-commit support

- Warns before staging when unrelated staged entries exist outside selected paths.
- Does not warn when staged entries are only `.braids.json` or selected mirror paths.
- Restores selected paths from a final tree into both index and worktree.
- Suppresses success message under `--quiet`.
- Reports staging errors without attempting broad rollback.

### Add

- First `add --no-commit` creates and stages `.braids.json` and mirror content.
- `HEAD` remains unchanged.
- Command-owned paths are clean after staging.
- Unrelated staged, unstaged, and untracked files are preserved.
- Existing unrelated staged files produce warning but do not block.
- Dirty `.braids.json`, dirty target, tracked target, untracked target content, and unresolved Git operation still block.
- Temporary remote cleanup remains active.
- Cleanup failure after staging returns staged-specific error wording.
- Quiet suppresses success confirmation.

### Pull / Update / Up

- Single mirror update stages `.braids.json` and mirror content without changing `HEAD`.
- No-op pull leaves index/worktree untouched.
- Alias invocations behave the same as `pull`.
- Branch/tag/revision strategy change stages config when content is identical.
- Upstream file addition, modification, deletion, and rename are represented in cached diff.
- No-path `pull --no-commit` stages multiple eligible mirror updates and reports locked skipped mirrors.
- No-path `pull --no-commit` is tested as sequential partial staging after one upfront all-target preflight: earlier successful mirrors remain staged if a later mirror conflicts or errors.
- No-path `pull --no-commit` warning tests assert that earlier same-invocation staged mirror paths do not trigger the unrelated-staged warning, while a genuinely unrelated pre-existing staged file does.
- All-target preflight blocks dirty eligible mirror before any update side effects.
- Dirty selected mirror path, dirty config, and unresolved Git operation still block.
- Existing unrelated staged files produce warning but do not block.
- Conflict state and output match current pull conflict behavior, including `.git/MERGE_MSG`.
- Non-conflict pull success path avoids unnecessary real config writes where practical.
- Quiet suppresses success confirmation.

### Remove

- `remove --no-commit` stages `.braids.json` and mirror deletion without changing `HEAD`.
- Command-owned paths are clean after staging.
- Unrelated staged, unstaged, and untracked files are preserved.
- Existing unrelated staged files produce warning but do not block.
- Dirty `.braids.json`, dirty mirror path, untracked mirror content, and unresolved Git operation still block.
- `--keep` keeps the remote only; mirror files are still staged for deletion.
- Remote inspect/remove cleanup failures after staging return staged-specific wording.
- Quiet suppresses success confirmation.

## Integration Coverage

Add or extend integration tests to cover at least:

- `add --no-commit` from the executable with unrelated staged, unstaged, and untracked work.
- `pull --no-commit` with a real upstream update and `HEAD` unchanged.
- `update --no-commit` alias behavior in at least one executable path.
- No-path `pull --no-commit` with two eligible mirrors and one revision-locked skipped mirror.
- No-path `pull --no-commit` without unrelated pre-existing staged files does not warn merely because earlier mirrors were staged by the same invocation.
- `remove --no-commit` staging deletion and preserving unrelated state.
- `remove --no-commit --keep` keeping the configured remote while staging file/config removal.
- Subdirectory invocation for at least one no-commit command to verify normalized path handling.
- Pull conflict under `--no-commit` to prove output and recovery state remain unchanged.
- Dirty owned-path blocker under no-commit while unrelated work remains preserved.

## Targeted Commands

Run narrow checks as relevant while iterating:

```bash
bazel test //internal/cli:cli_test
bazel test //internal/command/...
bazel test //internal/command:add_test //internal/command:refresh_command_test //internal/command:remove_test //internal/command:completion_test
bazel test //integration:scoped_state_test
bazel test //integration:lifecycle_test
bazel test //integration:conflict_test
```

If new integration test targets are added, run them directly before the full integration suite.

## Full Gate

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test --test_env=PATH //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
bazel test --test_env=PATH //integration/...
```
