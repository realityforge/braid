# CLI Progress Output Requirements

Status: accepted

## Mission

Add Ruby-Braid-like user feedback for long-running remote repository operations while preserving Braid's current separation between command data, diagnostics, and verbose Git tracing.

## Scope

- Add a global `--quiet` option that is incompatible with `--verbose`.
- Add normal-mode progress/info messages for remote/cache-contact operations and explicitly scoped local setup operations.
- Keep command data output usable for humans and scripts.
- Keep verbose Git argv tracing distinct from normal progress messages.
- Document the final output contract and Ruby migration impact.

## Out Of Scope

- Exact Ruby output wording, blank-line layout, or status banners.
- Broad progress wrapping around every Git command.
- Compatibility shims for command-local `--quiet` or command-local `--verbose`.
- Machine-readable output formats.
- Hydrating the cache from `braid setup`; setup remains a local remote configuration command.

## Locked Decisions

- `--quiet` is a global flag and must appear before the command name.
- `--quiet` and `--verbose` are mutually exclusive parse-time errors.
- Data commands still print their requested data: `version`, `status`, `diff`, and `help`.
- Errors always print to `stderr`.
- Safety/action messages such as conflict recovery instructions and warnings remain visible even under `--quiet`.
- Default progress/info messages use `stderr`; command data remains on `stdout`.
- Dot ticking for long operations is TTY-aware and must insert a newline before the completed message.
- Long-running TTY operations append progress dots every 5 seconds.
- Default progress must not print raw upstream URLs because URLs can contain credentials.
- Progress is semantic and command-level; `internal/gitexec` verbose tracing remains the implementation point for raw Git argv tracing.

## Command Surface Expectations

- `braid --quiet <command>` suppresses progress/info output for commands that support progress.
- `braid --quiet <command>` does not suppress warnings, errors, or required recovery guidance.
- `braid --quiet --verbose <command>` and `braid --verbose --quiet <command>` fail with a usage error.
- `braid <command> --quiet` remains an unknown command-local flag, matching current global flag placement rules.
- `braid --quiet version` still prints the version.
- `braid --quiet status` still prints status result lines while suppressing progress/info.
- `braid --quiet diff` still prints diff data while suppressing progress/info.
- `braid --verbose <command>` continues to print deterministic Git argv tracing and should not be suppressed by normal progress decisions.
- Existing non-progress command-result and recovery output remains visible under `--quiet` unless this plan explicitly reclassifies it as progress.

## Behavior Expectations

- Start/completed messages should describe the Braid operation, not raw Git commands.
- Progress messages should use concise operation verbs such as `fetching mirror`, `fetched mirror`, `pushing mirror`, `pushed mirror`, and `updated mirror`.
- Messages should identify the mirror by repo-root-relative mirror path where possible.
- Every command path that may contact a remote or cache should emit default progress on `stderr`, including `add`, `pull`, `push`, `sync`, `status`, `diff`, and setup/cache hydration paths.
- Successful no-op outcomes should receive completion messages only when remote work was performed; completed text may say `already up to date`.
- No-op outcomes should remain clear and should not be mislabeled as updates.
- Multi-mirror commands should make it possible to identify which mirror is being processed.
- Non-TTY output should avoid unbounded dot streams.
- Failure and cancellation paths must stop any progress ticker, emit a newline when needed after same-line dots, and must not print success completion messages.

## Output Contract Deep Dive

The command/operation matrix, stream classification, setup behavior, reporter lifecycle, and terminal-probe approach are specified in `30-output-contract.md`.

## Quality Gates

CI parity is required. The full gate is:

1. `bazel run @rules_go//go -- fmt ./...`
2. `git diff --exit-code`
3. `git --version`
4. `git merge-tree --write-tree "--merge-base=HEAD^{tree}" "HEAD^{tree}" "HEAD^{tree}"`
5. `bazel test --test_env=PATH //...`
6. `bazel run @rules_go//go -- vet ./...`
7. `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
8. `bazel test --test_env=PATH //integration/...`

Targeted tests must cover CLI parsing, quiet/verbose incompatibility, stdout/stderr placement, progress suppression, every output-contract operation row, and every stream class.

## Known Intentional Divergences

- The Go implementation will not preserve exact Ruby progress wording.
- The implementation will keep Go Braid's global flag model rather than adding Ruby-style command-local flags.
- Normal progress will be coarser than `--verbose` Git tracing.

## Open Questions Register

### Q-01

- status: resolved
- question: Which commands should emit default progress messages?
- context: `add`, `pull`, `push`, and `sync` perform user-visible write or remote-sync workflows. `status` and `diff` also may contact remotes or cache, but their primary purpose is producing data on `stdout`.
- options:
  - A: Only mutating or workflow commands: `add`, `pull`, `push`, `sync`, and possibly `remove` for local completion summaries.
  - B: Every command that may contact a remote or cache: `add`, `pull`, `push`, `sync`, `status`, `diff`, and setup/cache hydration paths.
  - C: Only commands explicitly mentioned in the Ruby-parity request: `add`, `pull`, `push`, and `sync` push/pull phases.
- tradeoffs: A gives useful feedback for long workflows while limiting data-command noise. B maximizes visibility but creates more stderr activity for commands often used in scripts. C is smallest but may leave some long cache/fetch paths silent.
- recommended_default: A, with no default progress for `status` or `diff` beyond preserving their data output; revisit if users report silent hangs there.
- user_decision: B. Every command that may contact a remote or cache should emit default progress, including `status`, `diff`, and setup/cache hydration paths, while preserving data output on `stdout` and quiet suppression for progress/info.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`

### Q-02

- status: resolved
- question: What exact operation names should the start/completed messages use?
- context: Progress text becomes part of the user-visible CLI contract even if exact Ruby parity is not required.
- options:
  - A: Use concise operation verbs: `fetching mirror`, `fetched mirror`, `pushing mirror`, `pushed mirror`, `updated mirror`.
  - B: Use command-oriented text: `pull started`, `pull completed`, `push started`, `push completed`.
  - C: Use Ruby-like phrasing where known, with Go-specific wording where workflows differ.
- tradeoffs: A is specific and avoids implying success too early. B is consistent but less precise for multi-phase commands. C helps migration familiarity but risks accidental exact-parity expectations.
- recommended_default: A.
- user_decision: A. Use concise operation verbs such as `fetching mirror`, `fetched mirror`, `pushing mirror`, `pushed mirror`, and `updated mirror`.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`

### Q-03

- status: resolved
- question: What dot tick interval should be used for long-running TTY operations?
- context: Dots should reassure users during network waits without spamming terminals or logs.
- options:
  - A: 2 seconds.
  - B: 5 seconds.
  - C: 10 seconds.
- tradeoffs: A feels responsive but can be noisy. B is a balanced default. C is quiet but less reassuring.
- recommended_default: B.
- user_decision: B. TTY progress dots should tick every 5 seconds.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`

### Q-04

- status: resolved
- question: Should `--quiet` suppress non-error warnings that are not required recovery instructions?
- context: Existing push provenance warnings are printed to `stderr` but do not necessarily stop the command.
- options:
  - A: Quiet suppresses progress/info only; warnings remain visible.
  - B: Quiet suppresses progress/info and optional warnings; errors and recovery instructions remain visible.
- tradeoffs: A is safer because users still see degraded behavior. B is quieter for automation but can hide useful context.
- recommended_default: A.
- user_decision: A. Quiet suppresses progress/info only; warnings remain visible.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`

### Q-05

- status: resolved
- question: Should successful no-op outcomes receive new completed messages?
- context: `push` already reports no local changes and not-up-to-date outcomes. `pull` can currently be quiet when nothing changed.
- options:
  - A: Print start/completed only when an operation performed remote work; completed text may say `already up to date`.
  - B: Print a completion summary for every selected mirror, including no-ops.
  - C: Keep no-op success quiet except for existing messages.
- tradeoffs: A gives closure without over-reporting purely local no-ops. B is most explicit but changes quiet successful all-pull behavior substantially. C is least disruptive but weakens the "completed" part of the request.
- recommended_default: A.
- user_decision: A. Print start/completed only when remote work was performed; completed text may say `already up to date`.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`
