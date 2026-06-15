# Global Verbose Flag Requirements

Status: accepted
Last updated: 2026-06-15

## Mission

Move `--verbose|-v` from repeated subcommand options to one global CLI option
that must appear before the command name. The implementation should simplify
the parsed invocation shape, preserve verbose tracing behavior, and keep CLI
help/docs aligned with the new command surface.

## Baseline Evidence

- `internal/cli/cli.go` currently stores `Verbose bool` on `AddOptions`,
  `UpdateOptions`, `RemoveOptions`, `DiffOptions`, `PushOptions`,
  `SetupOptions`, and `StatusOptions`.
- `internal/cli/cli.go` currently registers `--verbose|-v` separately in seven
  subcommand parsers.
- `internal/cli/cli.go` currently accepts global flags only before the command
  through `parseGlobal`.
- `internal/command/preflight.go` currently reassembles the scattered verbose
  fields through `verbose(inv)`.
- Command handlers and cache helpers currently consume verbose through either
  `verbose(inv)`, command option values, or `inv.Push.Verbose`.
- `README.md` documents the top-level command form, and
  `plans/go-port-braid/00-requirements.md` lists `--verbose|-v` on individual
  command syntaxes.

## Scope

In scope:

- Add `Verbose bool` to `cli.GlobalOptions`.
- Accept `--verbose` and `-v` only as pre-command global flags.
- Remove per-command verbose fields and parser registrations.
- Replace `command.verbose(inv)` and command-option verbose call sites with
  direct `inv.Global.Verbose` usage.
- Preserve existing verbose trace behavior for git execution, cache fetches,
  and temporary push repositories.
- Keep `braid --verbose version`, `braid -v version`, `braid --verbose help`,
  and `braid -v help` valid even though verbose has no observable effect there.
- Update help text, parser tests, command tests where needed, `README.md`, and
  `plans/go-port-braid/00-requirements.md`.
- Update the Go-port requirements compatibility/divergence language so the
  removal of post-command verbose placement is documented as intentional.

Out of scope:

- Adding environment-variable control for verbose logging.
- Changing verbose trace content or formatting.
- Keeping post-command `--verbose|-v` as a compatibility alias.
- Redesigning unrelated flag parsing behavior.
- Changing cache flag behavior.

## Command Surface And Behavior

New top-level form:

```bash
braid [--verbose|-v] [--no-cache | --cache-dir <path>] <command> [options]
```

Accepted examples:

```bash
braid --verbose add https://example.test/repo.git vendor/repo
braid -v update vendor/repo
braid --verbose --no-cache diff vendor/repo -- --stat
braid --cache-dir .cache --verbose status
```

Rejected examples:

```bash
braid add https://example.test/repo.git vendor/repo --verbose
braid update vendor/repo -v
```

Expected behavior:

1. `--verbose` and `-v` set `inv.Global.Verbose`.
2. Command-specific option structs no longer contain verbose fields.
3. Post-command `--verbose` and `-v` are unknown command flags.
4. In `diff`, arguments after `--` remain git diff passthrough arguments; a
   `--verbose` after that separator belongs to git diff, not Braid.
5. Command-specific usage strings omit `--verbose|-v`.
6. Top-level usage is the only help text that advertises `--verbose|-v`.
7. Existing cache flag conflict behavior remains unchanged.
8. Verbose trace propagation must remain observable for normal git execution
   and for at least one secondary git execution path such as cache fetches or
   push temporary repository work.

## Quality Gates

Targeted validation during implementation:

```bash
bazel test //internal/cli:cli_test
bazel test //internal/command:command_test
bazel test //cmd/braid:braid_test
```

Command tests must include explicit coverage that global `--verbose` reaches
the git execution layer. The coverage must prove trace output or equivalent
observable propagation for at least one normal command path and one cache or
temporary-repository path.

Required full local gate before implementation completion:

```bash
bazel run @rules_go//go -- fmt ./...
bazel test //...
```

## Intentional Divergences

- Previous command-local `--verbose|-v` placement is intentionally removed. This
  is a product CLI cleanup, not a library compatibility surface.
- `version` and `help` accept global verbose with no effect, matching the normal
  global option model.

## Open Questions Register

All design questions below were resolved in the planning interview and reviewed
through iterative plan review. No open questions remain.

| ID | Status | Question | Context | Options | Tradeoffs | Recommended Default | User Decision | Artifacts Updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | Should `--verbose` move to a global flag accepted only before the command? | Existing global flags are pre-command, while verbose is duplicated across seven command parsers. | Global only; keep command-local; support both. | Global only simplifies the contract; supporting both preserves legacy placement but keeps duplicate behavior. | Global only before the command. | Accepted. | Requirements, implementation plan, task board. |
| Q-02 | resolved | Should `-v` move with `--verbose` as a global alias? | Users already have a short verbose alias. | Move both; move only long form; remove short form. | Moving both preserves shorthand while making placement consistent. | Move `-v` with `--verbose`. | Accepted. | Requirements, implementation plan. |
| Q-03 | resolved | Should handlers use `inv.Global.Verbose` directly? | `command.verbose(inv)` only exists to collapse scattered per-command fields. | Direct global field; keep helper. | Direct usage makes the single source of truth visible. | Use `inv.Global.Verbose` directly and delete helper. | Accepted. | Requirements, implementation plan. |
| Q-04 | resolved | Should global verbose be accepted for `version` and `help` even though it has no effect? | Global flags normally parse before any command. | Accept no-op; reject for commands without trace output. | Accepting keeps global parsing simple and predictable. | Accept as no-op. | Accepted. | Requirements, implementation plan. |
| Q-05 | resolved | Should help advertise verbose only as a global option? | Current command-specific usage repeats `--verbose|-v` seven times. | Top-level only; duplicate in every command; duplicate plus global. | Top-level only keeps the CLI contract unambiguous. | Show verbose only in top-level usage. | Accepted. | Requirements, implementation plan. |
| Q-06 | resolved | Should docs and active requirements be updated with the behavior change? | README and Go-port requirements explicitly describe CLI syntax. | Update docs/requirements; code/tests only. | Stale syntax docs would mislead users and future work. | Update README and `plans/go-port-braid/00-requirements.md`. | Accepted. | Requirements, implementation plan. |
| Q-07 | resolved | Should implementation start after the design decisions are resolved? | The design is now concrete and scoped. | Implement now; stop after planning. | Implementation should wait for structured-plan review because the workflow was requested before code edits. | Emit plan now, then implement after review/approval. | Accepted with structured workflow requested. | Requirements, implementation plan, task board. |
