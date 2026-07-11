# Partial-clone cache implementation plan

## Phase sequence

1. Establish versioned config parsing and migration primitives.
2. Add CLI contracts and the `upgrade-config` handler.
3. Implement and verify partial-clone cache transport and validity.
4. Wire the mirror policy through add and every cache consumer.
5. Add end-to-end coverage and update user documentation.
6. Run exact CI-parity validation and inspect the final diff.

## Implementation details

### 1. Config model and version boundaries

- Extend `mirror.Options` and `mirror.Mirror` with `PartialClone`.
- Set `config.CurrentVersion` to 2 and add the optional JSON field to v2 mirror
  read/write types.
- Separate current loading from an exact v1 migration reader so ordinary config
  loading never silently upgrades old data.
- Give v1 errors an actionable `upgrade-config` diagnostic and preserve the
  distinct future-version error.
- Validate the invariant `PartialClone => RemotePath != ""` in both mirror
  construction and config validation.
- Unit-test v1/v2 boundaries, stable JSON, false omission, unknown fields, and
  invalid partial-clone combinations.

### 2. CLI and upgrade command

- Add `CommandUpgradeConfig`, `UpgradeConfigOptions`, parsing, main and command
  usage, handler registration, and Bash completion entries.
- Add `--partial-clone` parsing and usage to `add`, including the `--path`
  validation.
- Implement `UpgradeConfigHandler` using existing preflight, scoped cleanliness,
  temporary-index commit, and no-commit staging patterns rather than adding a
  second Git mutation path.
- Snapshot the original config worktree bytes and exact index entry before
  mutation and snapshot the original `HEAD` OID. Default mode should hash
  canonical v2 bytes, replace only the config item in `HEAD`'s tree, commit
  through `CommitTreeWithTemporaryIndex`, then restore `.braids.json` from the
  new `HEAD` so its real index and worktree are clean. `--no-commit` should write
  the bytes and update only the real index entry for `.braids.json`. On a
  failure before `HEAD` moves, restore both config snapshots. On a failure after
  it moves, first compare-and-swap `HEAD` from the new OID to the original OID,
  then restore both snapshots without touching unrelated state; combine any
  rollback failure with the primary error. Force each boundary in tests,
  including post-commit restoration failure, and assert original HEAD, config
  index entry/worktree bytes, and unrelated changes are identical.
- Make upgrade locate the repository root, distinguish missing/v1/v2/future
  files, render canonical v2 JSON, and commit only `.braids.json` by default.
- Test parsing, dispatch, output, clean/no-op/error cases, isolated commits, and
  staged `--no-commit` results with unrelated changes present.

### 3. Partial-clone cache mechanics

- Keep `RepositoryCachePath` stable across policy changes. Record owned metadata
  as `braid.cacheVersion=1` and `braid.partialClone=true|false`. Treat an
  otherwise-valid existing cache with no Braid metadata as legacy full mode;
  accept and annotate it when full mode is requested, rebuild it when partial
  mode is requested, and rebuild corrupt/unknown metadata.
- Extend repository-local cache readiness/creation to detect a full-versus-
  partial mismatch. While holding the existing per-mirror lock, retain the old
  cache at a deterministic same-filesystem backup while configuring and
  hydrating its replacement. Delete the backup only after hydration succeeds;
  restore it after any failure. At lock acquisition, a backup always wins over
  an incomplete stable replacement, so interruption deterministically restores
  the last known-good cache. Never delete the last valid cache before the
  replacement is usable.
- Configure the bare cache with the owned remote name `braid-upstream`, its URL,
  `remote.braid-upstream.promisor=true`, and
  `remote.braid-upstream.partialclonefilter=blob:none`; validate all four values
  as part of partial-cache readiness. Issue filtered shallow/full fetches
  through that named remote without a full-fetch fallback.
- For cache-to-downstream fetch, point the temporary mirror remote directly at
  upstream, configure it with
  `promisor=true` and `partialclonefilter=blob:none` and pass
  `--filter=blob:none`. Keep that remote until tree construction and worktree
  restoration have materialized the selected subtree, then remove it using the
  existing cleanup path. A later command recreates the configuration rather
  than leaving a dead promisor remote installed.
- Add a gitexec fetch-result method that returns captured stdout, stderr, exit
  status, and error while retaining the simple `Fetch` wrapper for existing
  callers. Partial hydration should reject both non-zero filter failures and
  warning-success output indicating that filtering was not recognized. Verify
  the postcondition with lazy fetching disabled (`GIT_NO_LAZY_FETCH=1`) and an
  object enumeration such as `rev-list --objects --missing=print`; do not use an
  object-existence command that can itself hydrate the known unrelated blob.
  Add focused local smart-protocol fixtures with `uploadpack.allowFilter`
  enabled and disabled, covering warning-success and hard-rejection results
  without network remotes or global Git config.
- Ensure cache cleanup/replacement and temporary remote cleanup remain correct
  after config, fetch, validation, candidate-promotion, backup-restoration, and
  downstream-materialization failures.

### 4. Command wiring and behavior tests

- Persist the add flag through `mirror.NewFromOptions` and config serialization.
- Ensure shared hydration honors the policy for add, pull, diff, push, sync,
  setup, and status while global/no-cache modes remain unchanged.
- Verify revision-, branch-, and tag-tracked mirrors continue resolving refs
  correctly through a partial cache.
- Verify selected-path trees and blobs materialize, unrelated large blobs remain
  absent from the cache/downstream object store with lazy fetching disabled,
  and subsequent pulls in a new process recreate temporary promisor state and
  reuse the valid cache after normal remote cleanup.
- Verify manually changing policy causes cache replacement rather than path
  proliferation or silent full-cache reuse.

### 5. Documentation and compatibility surface

- Document config version 2, `partial_clone`, its upstream-path restriction,
  cache-mode scope, server requirement, and the `add` flag in `README.md`.
- Update `docs/migration-from-ruby-braid.md` where it currently says the Go
  implementation has no `upgrade-config`; keep the distinction from Ruby
  legacy-format upgrades explicit.
- Update completion and usage snapshots/tests.

## High-risk areas

- Git may return success while warning that filtering was not recognized.
  Mitigation: test the exact supported and unsupported local protocol behavior,
  retain command stderr/result evidence where necessary, and assert that the
  resulting repository is genuinely configured and operating as a promisor.
- Both transport operations must remain filtered even though the downstream
  connects directly to upstream. Mitigation: assert absence of a known
  unrelated blob after materialization.
- Lazy object retrieval can fail if the temporary remote is removed too early.
  Mitigation: exercise add and pull through worktree restoration, verify the
  selected subtree contents before cleanup, and retain existing cleanup tests.
- Raising `CurrentVersion` can accidentally make migration parsing unreachable.
  Mitigation: keep explicit current and exact-v1 entry points with boundary
  tests.
- Upgrade commits can absorb unrelated staged or working changes.
  Mitigation: reuse temporary-index/scoped staging machinery and inspect commit
  trees in tests.
- Cache policy changes can leave obsolete data or stale remote configuration.
  Mitigation: stable cache identity, explicit validity metadata, recoverable rebuild,
  and setup/readiness regression tests.

## Validation strategy

Run narrow checks after each task, such as:

- `bazel test //internal/config:config_test //internal/mirror:mirror_test`
- `bazel test //internal/cli:cli_test //internal/command:command_test`
- focused `bazel test` filters for cache, add, pull, setup, completion, and
  upgrade cases as targets permit
- `bazel test //integration/...`

Before declaring implementation ready, run the repository-mandated checks in
this order:

1. `bazel run @rules_go//go -- fmt ./...`
2. `git diff --exit-code` (after formatting, to prove formatting made no
   unreviewed changes; inspect the intended feature diff separately)
3. `git --version`
4. `git merge-tree --write-tree "--merge-base=HEAD^{tree}" "HEAD^{tree}" "HEAD^{tree}"`
5. `bazel test --test_env=PATH //...`
6. `bazel run @rules_go//go -- vet ./...`
7. `bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run`
8. `bazel test --test_env=PATH //integration/...`

Because the implementation itself will have an intentional diff, the CI
format check should be reproduced by recording the pre-format diff, running
format, and confirming no additional diff, or from a clean implementation
commit/worktree. Do not misreport `git diff --exit-code` against intentional
uncommitted changes as a formatting failure.

## Completion criteria

- All task-board acceptance criteria are met and evidence is recorded.
- No open requirements questions remain.
- Exact CI-parity checks pass, or any unavailable check and residual risk are
  reported precisely.
- The final diff contains no scratch code or unrelated changes.
- No commit or pull request is created unless explicitly requested.
