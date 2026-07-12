# Git Transaction Deep Dive

## Pull

Represent upstream lookup as present `TreeItem` or absent. A replacement primitive inserts or removes an item and handles blob/tree/symlink/executable/file-directory transitions; gitlinks are rejected.

Prepare from one captured HEAD: resolve revisions, validate/retrieve every optional item, synthesize aggregate base/remote trees, merge once, and build config/final trees without repository mutation. Apply only after preparation. Snapshot HEAD/ref state, source/config index stages, worktree type/mode/content/symlink/obstructions, and MERGE_MSG. Clean success commits/stages together; conflict is the documented intentional state; all other apply failures restore the snapshot and HEAD with expected-old ref updates.

Conflict plumbing must retain full merge conflict records or use a temporary index to obtain exact stages 1/2/3. Copy only selected conflict/clean stages into the real index, preserve unrelated entries, materialize conflict worktree content, and stage config. Assert `ls-files -s/-u` and successful recovery.

Required tri-state cases: absent/absent/absent; absent base with local addition; present base plus absent remote with unchanged local; delete/modify conflict; mixed results across mirrors.

## Push

Build an upstream path trie after canonical validation. Equal paths compare present/absent plus type/mode/hash. Ancestor/descendant compares relative lookup inside the ancestor local tree; absent ancestor requires absent descendants. Validate all before side effects, then apply only outermost entries to recorded tree. Resolve the actual destination ref; require an existing target to equal recorded revision, allow absent create-if-absent, then push with explicit expected-old semantics.

## Cache

URL identity selects shared bare objects and lock. Source-scoped internal refs use encoded source name plus purpose and publish only after hydration. Readiness includes revision + policy + sorted upstream-path-set digest. Object writes may remain after failure, but ready refs may not. Test concurrency, differing tracking/revision/policy, same-revision topology change, separator equivalence, root/union hydration, and failure rollback.

## Sync

Preflight every selected source before upstream updates. Sources run by name and each push is one ref transaction. Later failure reports irreversible completed sources, current source, and safe recovery instructions.
