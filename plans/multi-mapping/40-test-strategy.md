# Test Strategy

## Focused coverage

- Config: exact schema, tracking variants, obsolete-v2 diagnostic, names, paths/overlap/case-fold, canonical ordering, v1 grouping/sanitization/suffixes.
- CLI/completion: both add modes, first `=` split, option rejection, selectors/cardinality from nested directories, aliases, mapping candidates, suppression.
- Lifecycle: atomic create/add/remove one/last/all, recorded revision, dirty scope, staged/unrelated isolation.
- Pull: clean/no-op/config-only/local/remote/conflict, complete tri-state deletion matrix, root/file/directory/mode transitions, no-commit and mutation failure rollback.
- Push: distinct/duplicate/nested/root representations, consistency diagnostics, symlink/executable modes, rejected gitlinks, root deletion, unmanaged preservation, exact target CAS including alternate/unborn/moved refs.
- Sync: deduplication/order, autostash, locked/non-branch sources, post-push pull failure, multi-source partial-progress diagnostics.
- Cache/partial clone: shared URL concurrency, source-scoped refs, different tracking/policy, union/root hydration, recorded recovery, failed readiness publication.
- Provenance: independent anchors, OID union/order/cap, add/remove/remap, tracking drift, URL identity, malformed/v1 boundary.
- Status/diff: source/mirror selection, content/source drift, missing paths, one resolution/fetch per source.

## Executable integration

- Multi-mirror create/add/remove/pull/push/sync, conflicts, no-commit, rollback, dirty/index isolation, and ordering.
- Duplicate/nested/root overlap and deletion; selector forms from subdirectories; emitted completion behavior.
- V1 grouping/naming in commit and no-commit modes; obsolete-v2 failure.
- Shared URL cache and partial-clone root hydration.
- Portable unit coverage plus the full CI matrix for Windows-sensitive paths.

## Completion matrix

- Status/diff/pull/push/remove/sync offer `:source` globally and mirror paths relative to cwd according to selector cardinality.
- After any sync alias selects a source, suppress its name and all mirror aliases.
- `add URL` completes ordinary filesystem local destinations and creation flags; after `add :source`, complete mapping local sides but suppress `--name`, tracking, and partial-clone flags.
- After `LOCAL=`, do not offer repository-local source selectors; upstream-path completion is not attempted because it would require remote tree access during shell completion.
- Single-selector commands stop selector completion once supplied; add/sync remain variadic under their stated rules.

## Required gates

Run plans-empty after cleanup, Git version/merge-tree probes, formatting plus clean diff, `bazel test --test_env=PATH //...`, vet, pinned lint, and `bazel test --test_env=PATH //integration/...` exactly as `.github/workflows/ci.yml`. Observe the four-platform matrix and aggregate `CI success` on the PR.
