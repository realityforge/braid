# Global Verbose Flag Implementation Plan

Status: accepted
Last updated: 2026-06-15

## Phase Sequence

1. Finalize this accepted plan after user review.
2. Move verbose parsing and invocation data to global options.
3. Update command handlers to consume `inv.Global.Verbose` directly.
4. Align help text, README, and active requirements syntax.
5. Run targeted tests while iterating, then run the full local gate.

## Delivery Approach

- Execute one task at a time with scoped diffs.
- Keep the parser model consistent with existing pre-command global flags.
- Avoid compatibility aliases for the removed post-command placement.
- Update tests in the same task as the behavior they verify.
- Implementation may proceed after the reviewed plan is accepted.

## Implementation Details

### Phase 1: Parser And Data Shape

- Add `Verbose bool` to `cli.GlobalOptions`.
- Extend `parseGlobal` to recognize `--verbose` and `-v`.
- Remove `Verbose bool` from command-specific option structs.
- Remove `boolFlag("--verbose", "-v", ...)` from `parseAdd`,
  `parseUpdate`, `parseRemove`, `parseDiff`, `parsePush`, `parseSetup`, and
  `parseStatus`.
- Update parser tests so accepted verbose examples use pre-command placement.
- Add parser coverage proving post-command `--verbose` and `-v` are rejected as
  unknown command flags.
- Add parser coverage for `braid --verbose version` and `braid -v help`.

### Phase 2: Command Behavior

- Replace all `verbose(inv)` call sites with `inv.Global.Verbose`.
- Replace command-option verbose arguments in update, status, diff, and push
  flows with `inv.Global.Verbose` or pass the global value into helper methods.
- Delete `command.verbose(inv)`.
- Keep verbose trace behavior unchanged inside `internal/gitexec`.
- Add explicit command-level validation that `braid --verbose ...` reaches the
  git execution layer for at least one normal command path and one cache or
  temporary-repository path. Prefer observable trace output assertions; use
  focused unit seams only if command-level trace assertions would be brittle.
- Run command tests to ensure cache, setup, push, and diff behavior still
  exercise the same paths.

### Phase 3: Help And Documentation

- Change top-level usage to advertise:

  ```text
  braid [--verbose|-v] [--no-cache | --cache-dir <path>] <command> [options]
  ```

- Remove `--verbose|-v` from command-specific usage strings.
- Update `README.md` command form and any prose that describes global flags.
- Update `plans/go-port-braid/00-requirements.md` command surface so the active
  Go-port requirements reflect the implemented CLI.
- Update the Go-port requirements compatibility/divergence language so
  post-command `--verbose|-v` removal is clearly intentional.

### Phase 4: Validation And Closeout

- Run targeted gates:

  ```bash
  bazel test //internal/cli:cli_test
  bazel test //internal/command:command_test
  bazel test //cmd/braid:braid_test
  ```

- Run required full local gate:

  ```bash
  bazel run @rules_go//go -- fmt ./...
  bazel test //...
  ```

- Update task-board evidence after each task.
- Commit only after task validation passes and commit approval/request is clear.
  If the user does not request commits for this feature, completed tasks must
  record `commit.hash: not_required` instead of remaining `pending`.

## High-Risk Areas

- Risk: a command-specific helper still reads a removed verbose field.
  - Impact: compile failure or lost verbose behavior in cache/temp repo paths.
  - Mitigation: compile through targeted command tests, search for `.Verbose`
    references after edits, and add explicit trace propagation assertions for
    normal plus cache or temporary-repository git paths.
- Risk: help text and parser behavior diverge.
  - Impact: users see invalid syntax or tests miss rejected placement.
  - Mitigation: update `CommandUsage` tests and add rejection tests for
    post-command verbose placement.
- Risk: diff passthrough accidentally consumes git arguments.
  - Impact: `braid --verbose diff path -- --stat` or git diff args regress.
  - Mitigation: keep parser separator behavior unchanged and retain diff
    passthrough test coverage.

## Required Full Gates

Run before marking implementation complete:

```bash
bazel run @rules_go//go -- fmt ./...
bazel test //...
```

## Completion Criteria

- `--verbose|-v` is accepted only before the command and stored in
  `inv.Global.Verbose`.
- Command-specific option structs have no verbose fields.
- Command handlers use `inv.Global.Verbose` directly.
- Post-command `--verbose|-v` is rejected.
- Top-level help advertises verbose as global, and command help does not repeat
  it.
- README and active Go-port requirements describe the final syntax.
- Targeted tests and full gate pass with evidence recorded.
- No task has `commit.hash: pending` after implementation is complete; tasks
  either record a real commit SHA or `not_required` when commits were not
  requested.

## Decision Log

- Q-01: Move `--verbose` to a pre-command global option only.
- Q-02: Move `-v` with `--verbose` as the global short alias.
- Q-03: Delete `command.verbose(inv)` and use `inv.Global.Verbose` directly.
- Q-04: Accept global verbose for `version` and `help` as a no-op.
- Q-05: Advertise verbose only in top-level usage.
- Q-06: Update README and active Go-port requirements syntax.
- Q-07: Emit this structured plan before implementation and wait for review.
