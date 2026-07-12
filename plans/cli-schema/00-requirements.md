# CLI Schema Requirements

## Problem

The parser, usage renderer, and completion engine maintain independent command
and option definitions. This allowed accepted aliases and help forms to be
missing from completion and requires tests to duplicate the interface manually.

## Scope

- Define one declarative schema in `internal/cli` for commands, aliases, global
  and command options, positional grammar, passthrough boundaries, visibility,
  static candidates, completion roles, and syntax-affecting constraints.
- Drive parsing, usage, completion, and generated completion contract cases
  from that schema.
- Resolve repository-aware completion roles in `internal/command` without
  introducing Git or configuration dependencies into `internal/cli`.
- Preserve current public behavior and diagnostics unless a schema-backed
  implementation exposes an existing inconsistency that must be corrected.

## Decisions

- The schema includes positional grammar and syntax-affecting semantic
  constraints; repository/config domain validation remains outside it.
- Coverage is exhaustive over structurally distinct CLI positions, not every
  permutation of unrelated options.
- Public aliases are completion-visible; internal commands are schema-declared
  but hidden. Removed interface elements receive no compatibility path.
- Help behavior is schema-wide.

## Acceptance Criteria

- [ ] Adding a visible command, alias, option, positional slot, or completion
      role to the schema automatically adds unit and executable contract cases.
- [ ] Parsing, usage, and completion no longer maintain separate command/flag
      inventories.
- [ ] Generated cases cover long and short names, separate and inline values,
      before/between/after positional slots, repeatable boundaries, passthrough,
      help, aliases, visibility, and every schema constraint transition.
- [ ] Focused behavior tests cover dynamic mirror and filesystem providers.
- [ ] All CI-parity gates pass.

## Out of Scope

- Additional shells beyond Bash.
- Repository-aware parsing or validation.
- Combinatorial testing of equivalent option permutations.
