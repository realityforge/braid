# Tag Mirror Ref Isolation Requirements

## Problem

Tag mirrors currently fetch `refs/tags/<tag>` into the downstream repository's
global `refs/tags/<tag>` namespace. Importing mirror content must not create or
overwrite a downstream tag.

## Scope

In scope:

- Fetch tag mirrors into a mirror-specific remote-tracking ref.
- Preserve lightweight and annotated tag resolution to the peeled commit.
- Cover normal, global-cache, and repository-local-cache command paths.
- Preserve `--keep`: a retained Braid remote keeps its tracking ref.
- Add a repository instruction requiring changes to land through pull requests.
- Deliver the implementation on a new branch through a pull request.

Out of scope:

- Deleting downstream tags created by earlier Braid versions.
- Changing branch or revision mirror behavior.
- Changing the global or repository-local cache's internal tag namespace; the
  defect concerns refs in the downstream repository.

## Behavior Requirements

1. A tag mirror's downstream fetch refspec is
   `+refs/tags/<tag>:refs/remotes/<braid-remote>/tags/<tag>`.
2. `Mirror.LocalRef()` returns the full
   `refs/remotes/<braid-remote>/tags/<tag>` ref for tag mirrors; resolution uses
   that ref and continues to peel annotated tags to commits.
3. Normal remote cleanup removes the mirror-specific tracking ref.
4. `--keep` retains the remote and its mirror-specific tracking ref.
5. Braid never creates, updates, or deletes `refs/tags/<tag>` in the downstream
   repository as part of this change.
6. Existing downstream tags remain untouched, including tags that may have been
   created by an earlier Braid version.

## Acceptance Criteria

- [ ] Adding and updating lightweight and annotated tag mirrors works in every
      cache mode without creating a downstream tag.
- [ ] Two tag mirrors with the same tag name do not share a downstream ref.
- [ ] A pre-existing, same-named downstream tag retains its original object ID.
- [ ] Normal cleanup leaves neither the temporary remote nor its tracking ref.
- [ ] `--keep` retains the mirror-specific tracking ref and no global tag.
- [ ] `AGENTS.md` states that changes must land through pull requests.
- [ ] All CI-parity checks pass and the branch is published as a PR.

## Decisions

- Q-001 resolved: use a mirror-specific remote-tracking ref and remove it with
  normal temporary-remote cleanup.
- Q-002 resolved: do not clean up previously created downstream tags because
  ownership cannot be determined safely.
- Q-003 resolved: retain the mirror-specific tag ref when `--keep` retains the
  remote.
