# Sync Autostash Implementation Issues

Status: active
Date: 2026-06-21

## I-01: Whole-Stash Index Apply Replays Unrelated Index State

- status: planned
- context: During T02, real-repo testing showed that a path-scoped
  `git stash push --all --pathspec-from-file=...` can still record unrelated
  staged index state in the stash index parent.
- evidence: `git stash apply --index <oid>` failed when an unrelated outside
  path had both staged and unstaged changes, attempting to replay that unrelated
  index state.
- why it matters: The accepted plan requires unrelated staged and unstaged state
  outside selected mirror paths to remain untouched.
- response: Keep staged selected mirror-path support, but do not use
  whole-stash `git stash apply --index`. Restore by applying the stash worktree
  state without `--index`, then restoring only selected mirror paths to the index
  from `<stash-oid>^2`.
- tracking tasks: T02, T03, T04.
- validation:
  - real Git plumbing test for selected staged/unstaged restoration,
  - real Git plumbing test for unrelated staged+unstaged preservation,
  - command test proving sync restores selected index state without mutating
    unrelated index state.

