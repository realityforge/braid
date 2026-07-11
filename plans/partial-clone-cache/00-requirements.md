# Partial-clone repository-local caches

## Problem

Repository-local mirror caches currently fetch the objects reachable from an
upstream revision. For mirrors that select one upstream subdirectory, large
blobs elsewhere in the repository add avoidable transfer and storage cost to
cache creation and refreshes.

## Scope

In scope:

- Add an opt-in, per-mirror Git partial-clone mode backed specifically by
  `--filter=blob:none`.
- Apply it only to repository-local caches and only to mirrors with an upstream
  `path`.
- Upgrade `.braids.json` to config version 2.
- Add an explicit `upgrade-config` command for version 1 files.
- Cover CLI, config, cache lifecycle, command wiring, completion, docs, and
  integration behavior.

Out of scope:

- Arbitrary Git object filters.
- Partial cloning for global caches or direct no-cache operation.
- Automatically choosing partial clone based on repository size.
- Enabling partial clone on an existing mirror through a command other than
  editing config explicitly.
- Upgrading legacy formats without `config_version` or versions other than 1.

## Interfaces

### `braid add`

- Add `--partial-clone` as a boolean flag.
- Reject it unless `--path <remote_path>` is also specified.
- Persist `"partial_clone": true` on the new mirror.

### `.braids.json` version 2

- Set `config_version` to `2` for all newly written configurations.
- Add optional mirror boolean `partial_clone`; omission means false.
- Reject `partial_clone: true` when the mirror has no non-empty `path`.
- Continue rejecting unknown fields and malformed mirror data.
- Ordinary commands reject version 1 with an actionable diagnostic directing
  the user to `braid upgrade-config`.

### `braid upgrade-config`

- Accept optional `--no-commit` and no positional arguments.
- Fail when `.braids.json` is missing.
- Read an exact version 1 config, preserve its mirror semantics, and write the
  canonical version 2 representation. The migration changes only the version
  when no other canonical formatting difference exists.
- Require `.braids.json` to be clean in both the index and worktree; unrelated
  changes are allowed.
- By default, create a commit containing only `.braids.json` with subject
  `Upgrade Braid config to version 2`.
- With `--no-commit`, stage only `.braids.json` and do not commit.
- Preserve every unrelated index entry and worktree file byte-for-byte. On a
  successful default upgrade, `.braids.json` matches the new `HEAD` and is clean
  in both index and worktree. On a successful `--no-commit` upgrade, its v2
  content is staged and also present in the worktree.
- If writing, staging, tree construction, committing, or restoration fails,
  restore the original `.braids.json` worktree bytes and index entry before
  returning the primary error. If `HEAD` moved before a later synchronization
  failure, compare-and-swap it back to its original OID before restoring the
  config snapshots, without changing unrelated index/worktree state. Include a
  rollback failure in the diagnostic.
- Treat an already-version-2 config as a successful no-op with concise output.
- Reject newer versions and unsupported older/missing-version layouts.

## Cache behavior

1. `partial_clone: true` is inactive when caching is disabled or the selected
   cache is global; those modes keep their existing behavior without error.
2. Every command that hydrates a repository-local cache uses the mirror policy:
   `add`, `pull`, `diff`, `push`, `sync`, `setup`, and `status`.
3. Upstream-to-cache hydration uses `--filter=blob:none`. The downstream fetch
   also uses `--filter=blob:none` but connects directly to upstream: serving a
   promisor cache through local upload-pack can recursively hydrate omitted
   objects and hang. The cache remains responsible for revision/ref metadata.
4. The cache is configured as a promisor/partial-clone repository so missing
   objects can be requested from upstream while Braid materializes the selected
   subtree.
5. Cache validity includes the configured partial-clone mode. A mismatched
   disposable cache is atomically rebuilt rather than reused, and changing mode
   must not create a permanently orphaned cache path.
6. If upstream does not support object filtering, the operation fails clearly;
   it must not silently fall back to a full fetch.
7. Existing full-cache behavior remains unchanged for mirrors where the field
   is absent or false.
8. Downstream promisor configuration is operation-scoped: it is installed
   before the filtered cache fetch, remains until the selected subtree is fully
   materialized, and is removed with the temporary mirror remote. A later Braid
   process must be able to recreate it and complete normally; ordinary Git use
   must not retain a dead promisor remote.

## Diagnostics

- Version 1 on ordinary commands: state that an upgrade is required and name
  `braid upgrade-config`.
- Invalid add/config combination: state that partial clone requires an upstream
  path.
- Unsupported server filtering: state that the configured partial clone could
  not be honored.
- Missing config during upgrade: state that `.braids.json` was not found.
- Preserve existing command cleanup semantics on fetch, commit, staging, and
  remote-removal failures.

## Acceptance criteria

- [ ] Version 2 round-trips `partial_clone` and omits it when false.
- [ ] Version 1 is accepted only by the upgrade path and receives an actionable
      error everywhere else.
- [ ] `add --partial-clone --path ...` persists the policy and rejects use
      without `--path`.
- [ ] A partial repository-local cache and its downstream fetch omit unrelated
      blobs while successfully materializing the configured subtree.
- [ ] A filter-incapable upstream fails rather than degrading to a full fetch.
- [ ] Cache mode mismatch triggers safe replacement at the same logical cache
      location.
- [ ] Failed cache replacement leaves the last valid cache usable, including on
      Windows directory-rename semantics, and interrupted swap artifacts are
      recovered deterministically.
- [ ] Global and disabled caches ignore the persisted policy and retain current
      behavior.
- [ ] `upgrade-config` commit, `--no-commit`, cleanliness, missing-file, no-op,
      version-error, rollback, and unrelated-index-preservation behavior are
      tested.
- [ ] CLI usage, Bash completion, README/config documentation, and migration
      documentation describe the new contracts.
- [ ] Every required CI-parity check passes.

## Decisions

All design questions are resolved:

- `partial_clone` / `--partial-clone` uses Git-native terminology while
  intentionally supporting only `blob:none`.
- Partial clone requires an upstream path.
- Upgrade commits by default, supports `--no-commit`, isolates unrelated
  changes, fails on a missing file, and is a no-op on v2.
- False is represented by omission.
- No compatibility fallback is provided for filter-incapable servers.

## Open questions

None.
