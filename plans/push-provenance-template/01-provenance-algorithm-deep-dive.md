# Push Provenance Algorithm Deep Dive

Status: accepted
Date: 2026-06-21

## Goal

Define a history-derived algorithm that can build useful downstream commit
guidance for an upstream push without storing new Braid metadata.

The algorithm intentionally answers:

> Which downstream commits touched this mirror since the last clean mirror state?

It intentionally does not answer:

> Which exact commits contributed each surviving line in the final upstream
> diff?

## Inputs

- Current downstream `HEAD`.
- Current configured `mirror.Mirror` selected for push.
- The recorded upstream revision already resolved by push.
- The downstream Git object database after push has fetched the selected mirror.
- Git's configured `core.commentChar`.

## Mirror Identity

Mirror identity is:

- local path from the config key,
- upstream URL,
- upstream path (`m.RemotePath`).

The following do not reset identity by themselves:

- recorded upstream revision,
- branch,
- tag,
- revision-locked tracking mode.

If first-parent history reaches a commit where the current local path is absent
from `.braids.json`, or the URL/upstream path differs, provenance collection
must stop at that boundary and must not inspect older commits.

## Clean Anchor Search

The clean anchor is the newest first-parent commit, walking backward from
`HEAD`, where:

1. `.braids.json` exists and parses successfully.
2. The current local path exists in `.braids.json`.
3. The mirror identity matches current local path, URL, and upstream path.
4. The downstream tree item at that commit's local path equals the upstream
   mirror item recorded by that commit's `.braids.json`.

The upstream mirror item for a historical commit is:

- the whole recorded revision tree when `RemotePath` is empty,
- the `ls-tree` item at `RemotePath` inside the recorded revision otherwise.

The downstream item for a historical commit is:

- the `ls-tree` item at the mirror local path inside that downstream commit.

The comparison must use object identity and mode/type information, not file
content string comparison.

If a clean anchor is found, the displayed history range is:

```text
<clean-anchor>..HEAD
```

If an identity boundary is reached before a clean anchor is found, the displayed
history range is:

```text
<identity-boundary>..HEAD
```

This keeps provenance from crossing into a previous mirror identity even when a
historical clean state cannot be established before the boundary.

If first-parent traversal reaches the root commit with matching mirror identity
and no clean anchor, collect all eligible commits reachable from `HEAD` that
touched the mirror path. The guidance block must include a commented note that
no clean mirror anchor was found and that all reachable commits for the current
mirror identity are shown, subject to the normal cap.

## Commit Collection

After determining the lower bound, collect eligible commits from full-history
path traversal, not first-parent-only history or Git's default simplified
path-history traversal:

```text
git log --full-history --reverse <range> -- <mirror path>
```

Collection requirements:

- Use full hashes in the guidance block.
- Preserve full commit messages.
- Display commits in chronological order.
- Include merge commits if the merge commit itself touched the mirror path.
- Include commits from merged branches if they touched the mirror path.
- Before display, re-check every candidate commit's `.braids.json`; exclude the
  candidate unless the local path, URL, and upstream path match the current
  mirror identity at that candidate commit.
- A candidate commit without `.braids.json`, without the current mirror path, or
  with a different URL/upstream path is an identity mismatch and is excluded
  without warning.
- A malformed historical `.braids.json` remains a provenance failure because the
  candidate's identity cannot be trusted.
- Exclude commits whose subjects start with:
  - `Braid: Add mirror `
  - `Braid: Update mirror `
  - `Braid: Remove mirror `
- Cap after filtering by retaining the newest 25 eligible commits.
- Display the retained set in chronological order.
- Include a commented note naming the number of older eligible commits omitted.

## Formatting

The template is guidance only, not a generated upstream message.

Shape:

```text
<comment> Braid downstream mirror commit guidance for <path>
<comment>
<comment> Commit <full hash>
<comment> <first commit message line>
<comment> <next commit message line>
<comment>
<comment> Commit <full hash>
<comment> <first commit message line>
```

Rules:

- Prefix every line with the effective comment character and one space.
- Preserve blank message lines as commented blank lines, for example `# `.
- Preserve commit message text line-for-line after adding the comment prefix.
- Do not add a non-comment subject or body.
- If there are no eligible commits, do not create a provenance template.
- If no clean anchor was found before root, include the no-anchor note as
  commented guidance before the commit entries.

## Comment Character Handling

Git strips only the configured comment character during commit cleanup. The
temporary push repository performs the actual `git commit -v`, so it must use
the same comment character as the downstream repository when guidance is
generated.

Git can be configured with cleanup modes such as `commit.cleanup=whitespace`
that preserve comment lines. When a provenance template is supplied, Braid must
force a cleanup mode that strips comment lines, such as `git commit
--cleanup=strip -v -t <template>`, regardless of the user's cleanup
configuration. The existing no-template commit path should preserve current
behavior.

Behavior:

- If `core.commentChar` is unset, use `#`.
- If `core.commentChar` is a single character, copy/set that value into the
  temporary push repository and use it for every guidance line.
- If `core.commentChar` is `auto`, skip guidance and warn.
- If Git returns an unexpected value, skip guidance and warn.

## Failure Handling

Provenance generation is best-effort. It must warn and return "no template" for:

- missing recorded revision objects needed for historical comparison,
- missing or malformed historical `.braids.json`,
- unsupported `core.commentChar`,
- unexpected Git command failures while walking history,
- inability to write the temporary template file.

The push itself must continue unless an existing push-critical step fails.

## Implementation Notes

- Add explicit `internal/gitexec` methods for new Git operations instead of
  invoking Git ad hoc from command code.
- Keep the algorithm in `internal/command` near push orchestration, likely in a
  focused helper file such as `push_provenance.go`.
- Reuse `config.Parse` for historical `.braids.json` blobs.
- Use temporary files for commit templates and clean them with the temporary
  push repository.
- Extend or wrap `CommitVerbose` so push can pass `-t <template>` when guidance
  exists and keep the current behavior when it does not.

## Known Limits

- Reverted or transient commits that touched the mirror path can appear.
- Blame-style per-line authorship is not attempted.
- A user-authored commit with a Braid automatic subject prefix is filtered out.
- If historical upstream objects are unavailable locally after the normal push
  fetch, guidance may be skipped with a warning.
