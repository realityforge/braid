# Braid Sync Command Requirements

Status: accepted
Date: 2026-06-21

## Mission

Add `braid sync` as a repository command that coordinates the existing push and
update workflows for selected mirrors:

1. precheck every selected mirror scope up front,
2. push committed local mirror changes for push-eligible mirrors,
3. pull/update every selected pull-eligible mirror.

The command should make the common "send local mirror changes, then record the
new upstream revision" workflow one command while preserving the safety and
scoped cleanliness properties already established for `braid update`.

## Evidence From Existing Code

- `internal/command/update.go` already supports no-path repository-wide updates,
  skips locked mirrors in no-path mode, and prechecks all eligible targets before
  doing update work.
- `internal/command/push.go` currently pushes exactly one mirror, opens Git's
  commit editor for the upstream commit, and treats "not up to date" and "no
  local changes" as non-error early stops.
- `internal/command/update.go` defines scoped cleanliness through
  `ensureCommandScopesClean`: block unresolved Git operations, require
  `.braids.json` clean, and require each target mirror path clean.
- `internal/config/config.go` exposes sorted `cfg.Paths()` ordering for
  repository-wide commands.
- `internal/cli/cli.go` currently has no `sync` command or multi-path command
  parser.

## Scope

In scope:

- Add CLI support for `braid sync [local_path...] [--pull-only] [--keep]`.
- Allow zero or more mirror path arguments.
- No paths means repository-wide sync over the same branch/tag mirror set used by
  no-path `braid update`.
- Explicit paths are normalized relative to the process directory using the same
  path semantics as existing commands.
- Explicit path order is preserved.
- Duplicate explicit paths are rejected.
- Run scoped prechecks for every selected mirror path before any push, fetch,
  setup, remote, cache, worktree, or commit side effect.
- Default sync performs a push phase followed by a pull/update phase.
- `--pull-only` skips the push phase and still performs the same up-front
  precheck.
- Push phase auto-pushes only branch-tracking mirrors with committed local
  mirror-content changes.
- Unchanged push-eligible mirrors are skipped quietly.
- A selected mirror that is not pushable by `sync` must not hide committed local
  changes that the user probably intended to send, whether selected explicitly
  or by no-path sync.
- Push planning must classify committed local mirror-content changes for every
  selected target before any upstream push, editor, worktree, config, or commit
  side effect. If required objects are missing locally, `sync` may first perform
  bounded object hydration after scoped precheck.
- Committed deletions inside an existing mirror directory are local mirror
  content changes; committed deletion of the selected mirror path itself is not
  pushed by `sync` and must fail with a clear diagnostic.
- Pull phase reuses existing update behavior and one-update-commit-per-mirror
  history shape.
- `--keep` retains temporary Braid remotes across both phases; default behavior
  removes them as existing `push` and `update` do.
- Update README, CLI usage, unit tests, and integration coverage.

Out of scope:

- No `--branch` flag for `sync`.
- No automatic push target for tag-tracking or revision-locked mirrors.
- No batching multiple upstream commits into one editor prompt or one message.
- No parallel sync execution.
- No fallback compatibility shims for old or removed command behavior.
- No change to `braid push`, `braid update`, `braid status`, or `braid diff`
  public behavior except internal refactoring needed for `sync`.

## Command Surface

```bash
braid sync [local_path...] [--pull-only] [--keep]
```

Global flags remain global-only and must appear before `sync`:

```bash
braid [--verbose|-v] [--no-cache | --cache-dir <path>] sync [local_path...] [--pull-only] [--keep]
```

Behavior:

- `braid sync` selects every configured branch/tag mirror, sorted by
  `.braids.json` path.
- `braid sync vendor/a vendor/b` selects those mirrors in that order.
- `braid sync --pull-only` selects every configured branch/tag mirror and only
  updates/pulls.
- `braid sync --pull-only vendor/revision` is allowed and follows existing
  explicit `update <path>` behavior, including no-op revision-locked updates.
- `braid sync vendor/revision` is allowed only when the mirror has no committed
  local changes to push; otherwise it must fail with an explicit diagnostic
  because `sync` has no `--branch`.
- `braid sync` must also fail when a selected tag mirror has committed local
  changes, including tag mirrors selected by no-path sync.

## Behavior Requirements

1. `sync` must run inside a Git working tree and require `.braids.json`, matching
   other repository/config commands.
2. `sync` must validate the loaded config with the existing config path rules.
3. `sync` must reject duplicate explicit mirror paths after normalization.
4. `sync` must reject missing mirror paths with the existing
   `mirror does not exist: <path>` error style.
5. `sync` must reject mirror paths that overlap `.braids.json` before side
   effects.
6. `sync` must block unresolved Git operation state before scoped path checks.
7. `sync` must require `.braids.json` and every selected mirror path to be clean
   in both index and working tree before any operational phase starts.
8. No-path `sync` must skip revision-locked mirrors before precheck, matching
   no-path `update`.
9. Explicit `--pull-only` revision-locked mirrors must use existing
   `update <path>` behavior.
10. After scoped cleanliness precheck, default sync may perform bounded object
    hydration for selected mirrors whose recorded revisions are missing locally.
    Hydration may set up/fetch temporary Braid remotes and update the local
    cache, but must not push upstream, invoke an editor, write the worktree or
    `.braids.json`, or create commits.
11. If bounded hydration cannot make a selected recorded revision available,
    `sync` must fail with a clear diagnostic before push-plan validation.
12. After scoped cleanliness precheck and any required object hydration, default
    sync must build and validate a complete push plan for all selected targets
    before pushing any mirror.
13. Push planning must detect whether downstream `HEAD` content at the selected
    mirror path differs from the recorded mirror revision before checking
    upstream freshness.
14. Push planning must use the same remote-path-aware item semantics as existing
    push/update/diff behavior: compare downstream `HEAD` at `m.Path` with the
    recorded upstream item at `m.RemotePath`, or with the whole recorded revision
    when `m.RemotePath` is empty. This must work for directory mirrors,
    subdirectory mirrors, and single-file mirrors.
15. Push-plan validation must check upstream freshness for every changed branch
    mirror before any push action or editor invocation. If any changed branch
    mirror is not up to date, default sync must fail the whole push plan before
    pushing earlier changed mirrors.
16. Branch mirrors with no committed local mirror-content changes must stay
    eligible for the pull/update phase even if upstream has moved.
17. Any selected non-branch mirror with committed local mirror-content changes
    must fail default sync before any push side effect, including no-path tag
    mirrors.
18. A selected mirror path absent from downstream `HEAD` must fail default sync
    push planning with a clear diagnostic instead of surfacing a low-level
    `ls-tree` error. Deletions inside an existing mirror directory remain
    supported local mirror content changes.
19. Push phase must run before pull phase.
20. Push phase must process planned push actions sequentially in target order.
21. Pull phase must process paths sequentially in target order.
22. If any operation fails after precheck and push-plan validation, `sync` must
    stop immediately and not continue to later mirrors or later phases.
23. If a branch mirror has committed local mirror-content changes and upstream
    has moved since `.braids.json`, `sync` must fail hard before pull/update and
    tell the user to update, resolve, commit, and retry.
24. If a branch mirror has no committed local mirror-content changes, `sync`
    must not print the existing single-mirror push "No local changes" message.
25. If a branch mirror is pushed successfully, `sync` must still update/pull that
    mirror so `.braids.json` records the newly pushed upstream revision.
26. Each actual push keeps the existing one-editor-invocation-per-upstream-commit
    behavior.
27. `--pull-only` must not invoke push planning, the push phase, or Git editor.
28. `--keep` must apply consistently to remotes used by object hydration, push,
    and update internals.
29. `RequirementsFor(cli.CommandSync)` must classify sync as
    `Requirements{Git: true, Root: true, Config: true, MayWrite: true}`.

## Diagnostics

- Duplicate path: fail as a usage/command error that names the duplicated
  normalized path.
- Not up to date during sync push: fail as a sync error, not a success message.
  The diagnostic must name the mirror path and direct the user to update, resolve
  conflicts if needed, commit, then rerun sync.
- Explicit non-branch mirror with committed local changes during non-pull-only
  sync: fail and direct the user to run `braid push <path> --branch <branch>` or
  rerun with `--pull-only` if they only intended to update.
- No-path tag mirror with committed local changes during non-pull-only sync:
  fail before any push side effect with the same non-branch local-change
  diagnostic.
- Selected mirror path removed from downstream `HEAD`: fail before any push side
  effect and tell the user that `sync` cannot push deletion of the mirror path
  itself.
- Scoped cleanliness errors should reuse the existing
  `local changes are present in <path>` diagnostic.

## Quality Gates

Required full gate:

```bash
bazel test //...
```

Targeted gates:

```bash
bazel test //internal/cli:cli_test
bazel test //internal/command:command_test
bazel test //integration:braid_integration_test
```

## Known Intentional Divergences

- Existing `braid push <path>` returns success when upstream moved; `braid sync`
  must treat that state as a hard failure when committed local mirror changes
  exist.
- Existing `braid push <path>` prints "No local changes" for unchanged mirrors;
  multi-mirror `sync` skips unchanged push targets quietly.
- Existing explicit commands accept one path; `sync` accepts zero or more paths.
- Existing `push` supports `--branch`; `sync` intentionally does not.

## Acceptance Criteria

- [ ] `braid help` and `braid sync help` document the new command.
- [ ] CLI parsing accepts `sync`, `--pull-only`, `--keep`, and zero or more
      normalized local paths.
- [ ] Explicit duplicate paths are rejected after normalization.
- [ ] No-path `sync` prechecks branch/tag mirrors up front and skips locked
      mirrors.
- [ ] Explicit `sync` prechecks exactly the named mirrors in user order.
- [ ] Fresh-clone sync can hydrate missing recorded revision objects after
      scoped precheck and before push-plan validation.
- [ ] Default `sync` validates the complete push plan before any upstream push,
      editor, worktree, config, or commit side effect.
- [ ] Default `sync` pushes branch mirrors with committed local changes, then
      updates every selected pull target.
- [ ] `--pull-only` updates selected mirrors without pushing or opening an
      editor.
- [ ] Branch mirrors with no committed local changes update normally when
      upstream moved.
- [ ] Sync classification handles branch mirrors with `--path` subdirectory
      mirrors and single-file mirrors, both changed and unchanged.
- [ ] Pushed mirrors are subsequently updated so `.braids.json` records the new
      upstream revision.
- [ ] Upstream-moved plus local committed changes fails hard during sync.
- [ ] A later changed branch mirror with moved upstream prevents earlier changed
      branch mirrors from being pushed or opening an editor.
- [ ] Selected non-branch mirrors with committed local changes fail in
      non-pull-only sync, including no-path tag mirrors.
- [ ] Selected mirror paths removed from downstream `HEAD` fail with a clear
      sync diagnostic.
- [ ] `--keep` retains temporary remotes used by sync.
- [ ] README documents sync behavior, target selection, `--pull-only`, and
      safety diagnostics.
- [ ] Targeted gates and `bazel test //...` pass before implementation is marked
      complete.

## Open Questions Register

All questions from the grill-me pass are resolved; no implementation-blocking
questions remain.

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | For `braid sync` with no paths, should the target set match `braid update` and skip revision-locked mirrors? | `update` skips locked mirrors in no-path mode; locked mirrors have no tracked remote ref. | Match update; include locked; fail if locked encountered. | Matching update preserves existing no-path semantics; including locked creates unclear pull behavior. | Match `update`: no-path sync targets branch/tag mirrors only. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-02 | resolved | Should `sync` support `--branch`, or only auto-push branch-tracking mirrors? | Existing `push` needs `--branch` for tag/revision mirrors, but one branch flag is ambiguous for multi-path sync. | Add `--branch`; branch mirrors only; infer branches. | Omitting `--branch` keeps sync simple and avoids ambiguous multi-mirror push targets. | Do not add `--branch`; auto-push only branch-tracking mirrors. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-03 | resolved | Should `sync` run all selected mirror prechecks up front before any push or pull side effects? | `update` already does this for update-all to avoid partial progress caused by a later dirty target. | Up-front precheck; per-mirror precheck. | Up-front precheck prevents needless network/setup/commit side effects before known scoped dirtiness. | Precheck all selected scopes first. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-04 | resolved | Should `sync` push only mirrors whose downstream `HEAD` has mirror-content changes relative to the recorded revision? | Existing `push` prints a no-op message for one mirror; multi-mirror sync would be noisy. | Push only changed; invoke push for all and print no-ops. | Quiet skipping keeps sync output focused; changed detection must be reliable. | Push only mirrors with committed local changes; skip unchanged quietly. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-05 | resolved | After a successful push, should `sync` still run pull/update for that mirror? | Existing workflow needs update after push so `.braids.json` records the pushed revision. | Always update; skip just-pushed mirrors. | Updating records pushed revisions and picks up remote-only changes. | Always run the pull/update phase for selected pull targets. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-06 | resolved | If one mirror fails during push, should `sync` stop immediately and skip remaining pushes and pulls? | Existing multi-mirror `update` stops on first operational error after precheck. | Stop immediately; continue best-effort. | Stopping keeps partial state easier to reason about. | Stop on first operational error. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-07 | resolved | What should happen if a selected branch mirror has committed local changes but upstream moved since `.braids.json`? | Existing `push` treats this as success with a message; continuing sync would then pull/update and obscure intended push work. | Preserve push behavior; hard sync failure. | Hard failure protects sync's push-then-pull contract. | Treat as hard sync failure and stop before pull/update. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-08 | resolved | If `sync` pushes more than one mirror, should it open Git's commit editor once per pushed mirror? | Mirrors may point at different upstream repos/branches; current push creates one upstream commit per mirror. | One editor per pushed mirror; shared message; no editor. | Per-mirror editor preserves current behavior and maps cleanly to upstream commits. | Reuse existing per-mirror push behavior. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-09 | resolved | Should `--pull-only` use the same locked-mirror rules as `update`? | No-path update skips locked mirrors; explicit update of a locked mirror is a no-op unless strategy flags are supplied. | Same as update; reject locked always; include locked no-path. | Matching update is predictable and avoids special-case no-op failures. | `--pull-only` follows update target rules; default sync rejects selected non-branch mirrors with local changes. R1 broadened the original explicit-path wording to include no-path tag mirrors. | Accepted recommendation; R1 broadening applied. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-10 | resolved | Should `sync` expose `--keep`? | `push` and `update` both support retaining temporary Braid remotes. | Add `--keep`; omit it. | Adding it preserves existing cleanup mental model across combined phases. | Add `--keep`; default removes temporary remotes. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-11 | resolved | For explicitly named mirrors, should `sync` process them in user order and reject duplicates? | No existing multi-path command pattern; no-path commands use sorted config order. | User order with duplicate rejection; sorted; allow duplicates. | User order matters for editor prompts and stop-on-first-failure semantics; duplicates would repeat side effects. | Preserve user order and reject duplicates. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
| Q-12 | resolved | Should this now be emitted as structured plans for review? | The behavior decisions are sufficiently specific for implementation planning. | Emit plan; keep grilling; start coding directly. | Planning artifacts make review and later implementation auditable. | Emit structured plan artifacts first. | Accepted recommendation. | `00-requirements.md`, `01-sync-command-deep-dive.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `40-test-strategy.md` |
