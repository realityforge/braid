# Multi-Mirror Source Requirements

## Objective

Model one named upstream Git source at one recorded revision with multiple non-overlapping local mirrors, preserving atomic source-wide pull/push/sync behavior and path-scoped inspection.

## Domain and configuration

- Replace `internal/mirror` with explicit `internal/source` types: `Source`, `Mirror`, `Tracking`, and `SourceSelection`. A source owns name, cleaned fetch URL, tracking, revision, partial-clone policy, and `[]Mirror`; it never carries an active path. A mirror owns `LocalPath` and `UpstreamPath`.
- Config v2 intentionally replaces the unreleased old Go v2 shape. Old top-level `mirrors` v2 fails with an obsolete-unreleased-schema diagnostic; only v1 is accepted by `upgrade-config`.
- Canonical branch-tracking JSON is:

```json
{
  "config_version": 2,
  "sources": {
    "replicant": {
      "url": "https://github.com/replicant4j/replicant.git",
      "branch": "master",
      "revision": "18480c9dc34f948218a0c15370712d27b2626fa0",
      "mirrors": {
        "licenses/replicant-LICENSE.txt": "LICENSE.txt",
        "vendor/libs/replicant": ""
      }
    }
  }
}
```

- `url`, `revision`, and non-empty `mirrors` are required. Zero or one of `branch`/`tag` is present; neither means revision-locked. `partial_clone` defaults false. Reject unknown/null/empty required fields and conflicting tracking.
- Canonical writes omit false `partial_clone` and order source names and local mirror paths lexicographically.
- Names match `[A-Za-z0-9][A-Za-z0-9._-]*`, excluding `.` and `..`; names are immutable and unique.
- Local paths never overlap globally. Upstream paths may overlap/repeat; `""` means root. Stored paths retain current portable separator, traversal, `.git`, `.braids.json`, case-fold, Windows-reserved, colon, and trailing-dot/space protections. Overlap is equality or component ancestry after canonical validation.
- Clean fetch URL by trimming trailing `/` or `\` only when this does not change a filesystem/URI root. Preserve `/`, `\`, Windows drive roots such as `C:\`, and `file:///`; otherwise trim trailing separators and preserve the result verbatim for Git. The same cleaned value is the identity key for grouping/cache/provenance equality. Cover POSIX root, Windows drive root, file URI root, SCP, ordinary URI, and local-path cases.
- Domain config uses explicit name/path APIs; only command resolution understands `:name` and cwd-relative paths.

## CLI and selection

- `braid add URL [--name NAME] [LOCAL[=UPSTREAM]...]` creates a source. Derive an unsanitized strict name from URL basename when omitted; fail on invalid/collision. With no mirrors, create one root mirror at explicit/derived name.
- Name derivation cleans trailing separators, converts `\` to `/` only for basename extraction, takes text after the final `/` (which naturally handles SCP `host:path/repo.git`), removes one `.git` suffix, and does not parse/remove query or fragment text. Windows/local paths follow the same separator rule. Empty/invalid results require `--name`. The explicit/derived name is also the zero-mirror local path.
- `braid add :source LOCAL[=UPSTREAM]...` requires mirrors, uses the recorded revision, and rejects `--name`, tracking, and partial-clone creation options.
- Remove obsolete `--path`; reject `=` in local paths and split mapping arguments on the first `=`.
- `status`/`diff`: zero or one selector; zero means all sources, path means one mirror, `:source` means all its mirrors.
- `pull`: zero or one selector; zero processes eligible sources by name and reports skipped locked sources; explicit path/name operates source-wide. Tracking flags require a selector and mutate source tracking.
- `push` and `remove` require exactly one selector. Push is source-wide. Remove by name removes the source; remove by path removes one mirror; removing the last removes the source.
- `sync`: zero or more selectors, deduplicates aliases to sources, runs by source name, and zero means all eligible sources. Explicit locked sources are processed under existing locked-source rules.
- Removing a mirror stops managing its upstream path; later push preserves that upstream content.

## Pull and transaction postconditions

- Pull fetches/resolves once and uses one aggregate three-way tree merge: base is HEAD with recorded mirror items, local is captured HEAD, remote is HEAD with candidate mirror items.
- Use present/absent tree items. Missing paths fail add; later disappearance deletes the mirror; absent-at-both is up to date. Support file/directory transitions, executable and symlink modes; reject gitlinks.
- Prepare is mutation-free. Pre-apply failures preserve HEAD, config bytes, index, worktree, MERGE_MSG, remotes, and unrelated state.
- Clean success creates/stages one source-wide result. No-op changes nothing. Conflict intentionally leaves successful mirror merges staged, conflicted mirrors unmerged with conflict content, new revision staged, one MERGE_MSG, and unrelated state unchanged.
- Operational apply failure rolls all source/config paths, index stages, worktree content, and MERGE_MSG back to their exact pre-operation state.
- Conflict application materializes true index stages 1/2/3 for conflicted paths using full merge-tree conflict records or an isolated temporary-index three-way read; clean paths/config are staged without replacing unrelated index entries. Recovery succeeds with the documented `git add` and commit flow.
- Rollback captures HEAD OID and symbolic/detached ref state and restores it with expected-old `update-ref` if failure occurs after ref advancement. It also restores file type, executable mode, symlink target, and file/directory obstruction state. Ignored entries under selected paths are preflight blockers unless explicitly preserved by autostash. New unreachable Git objects and external hook side effects are outside rollback; internal automatic commits continue using the repository's existing hook policy.

## Push and sync

- Reconstruct from the recorded upstream tree and preserve unmanaged content.
- Validate all duplicate/overlapping representations before editor/temp commit/push. Equal paths compare presence and complete item. Ancestor/descendant compares the descendant-relative item inside the ancestor tree; file ancestors and inconsistent absence fail with both representations named. Apply only outermost entries; absent root yields Git's empty tree.
- Resolve the exact destination branch, including `--branch`, and update with expected-old compare-and-swap/lease. An existing destination must equal the recorded source revision; diverged/ahead/behind/unrelated destinations fail without mutation. An unborn branch uses create-if-absent. Movement after planning fails by lease.
- Push creates at most one upstream update per source. Sync preflights all selected local state before external mutation and never partially pushes mirrors within a source. A successful upstream push cannot be rolled back if following pull fails; report completed sources and recovery steps.

## Cache, provenance, and status

- One object repository and lock exist per URL identity. Source-scoped ref namespaces include source name and purpose so shared-object users cannot overwrite recorded/requested/tip state. Differing tracking and partial-clone policy share objects but keep independent readiness refs.
- Hydration atomically covers every mirror needed at recorded and candidate revisions. Failed hydration publishes no ready refs. Root hydration uses no pathspec and materializes the complete current tree.
- Readiness is keyed by a deterministic digest of revision, partial-clone policy, and sorted normalized upstream mirror path set, so same-revision add/remap/root promotion cannot reuse incomplete hydration. Removal may reuse a superset safely.
- Provenance continuity requires name, URL identity, and tracking identity (branch name, tag name, or revision-locked kind). Each current mirror walks its own unchanged local/upstream identity window, stops cleanly at v1, filters Braid topology/source commits, unions by OID, orders deterministically oldest-to-newest/topologically, and applies the cap once after union.
- Status resolves latest source revision once, then reports each mirror's content drift separately from source revision drift. Locked sources have no remote revision drift.
- Mirror content compares optional recorded (`B`), local (`L`), and latest (`R`) items using complete type/mode/hash equality: if `L == R`, state is Up To Date even when both differ from `B`; else if `L == B`, state is Removed Remotely when `R` is absent and Modified Remotely otherwise; else if `R == B`, state is Removed Locally when `L` is absent and Modified Locally otherwise; else state is Modified Locally And Remotely. This truth table includes all-present and presence/absence cases. Source revision state is Current, Behind, or Locked.

## Upgrade and outputs

- V1 grouping compares cleaned URL, branch, tag, and revision exactly; v1 has no partial-clone field. Order groups by smallest local path.
- Upgrade-only naming replaces invalid runs with `-`, trims to a valid alphanumeric start/end, falls back to `source`, then applies case-sensitive `-2`, `-3`, ... suffixes. Ordinary add never silently sanitizes.
- Source transactions use source terminology; status/diff and topology changes use mirror terminology where path-specific.
- Update README, help, completion, migration docs, diagnostics, fixtures, and tests.

## Acceptance criteria

- Focused and executable integration tests cover lifecycle, overlap, deletion, conflict/rollback, selector, cache, provenance, status, upgrade, and output contracts.
- No `Mirror.Path`/`RemotePath`, ambiguous config lookup, obsolete v2 JSON, or `--path` compatibility remains.
- Every exact local command in `.github/workflows/ci.yml` passes; PR observes all platform checks and aggregate success.
- Implementation-alignment review has no actionable findings.
