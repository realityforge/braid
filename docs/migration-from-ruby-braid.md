# Compatibility and Migration from Ruby Braid

This document compares the current Go/Bazel Braid tool with the original Ruby
Braid tool from <https://github.com/cristibalan/braid>. It is for users deciding
whether to migrate and for maintainers checking what behavior might change.

Comparison baseline:

- Original Ruby Braid: `cristibalan/braid` `v1.1.10`
  (`16729390a2a8e6b45919545b056a1a7ac83c14d6`).
- Current Go Braid: the development version after `v0.9.8`
  (`52e331f5bd9285035c3edb364de2a67d52dbaddc`).
- Initial Go-port migration notes:
  <https://github.com/realityforge/braid/blob/d024c502c5eb988f47ac54944f0c5be16bd3b045/docs/migration.md>.

The Go tool records named upstream sources in `.braids.json`. Each source can
provide one or more local mirrors in the downstream repository. Its main
workflows are `add`, `status`, `diff`, `pull`, `push`, `sync`, and `remove`.
`pull` is the preferred spelling for updating mirror content; `update` and `up`
remain accepted aliases.

In short, existing Ruby Braid repositories need a one-time config migration,
scripts need a few command-line changes, and exact console output is not
compatible. The sections below explain the changes that affect everyday use and
automation.

## High-Impact Changes

### Native binary distribution replaces RubyGems

Ruby Braid is installed as a Ruby gem and needs a compatible Ruby, RubyGems, and
runtime gem environment. The Go tool is released as raw platform binaries with
`SHA256SUMS`; it does not need Ruby or Go on the target machine.

Current release automation builds native artifacts for Linux amd64/arm64, macOS
amd64/arm64, and Windows amd64. Release binaries are stamped to print the
release version, for example `braid 0.9.4`. A local unstamped Bazel development
run prints `braid 0.0.0-dev`.

Migration impact:

- Remove `gem install braid`, `bundle exec braid`, and Gemfile pinning from
  project setup if they only existed for Braid.
- Install the platform binary on `PATH` and verify it with `SHA256SUMS`.
- Check the Git version. Ruby Braid required Git 2.8.0 or newer; current Go
  Braid requires Git 2.45.0 or newer because pull conflict handling uses newer
  `git merge-tree` behavior.

### Commands can run from subdirectories

Ruby Braid refused to run unless the current directory was the downstream Git
worktree root. Current Go Braid commands run from any directory inside the
worktree.

When a command takes a mirror path, the path is resolved relative to the
directory where Braid was invoked and stored in `.braids.json` as a
repo-root-relative path. Commands without a path, such as `braid status`,
`braid diff`, `braid pull`, and `braid sync`, still operate on
the repository-wide mirror set.

Migration impact:

- Existing root-level workflows still work.
- Scripts that assumed Braid would reject subdirectory execution may need to
  normalize their working directory or pass explicit paths.
- Conflict recovery commands printed by Go Braid are written for the directory
  where Braid was invoked.

### Automatic commits preserve unrelated work

Ruby Braid's write commands generally required the whole repository to be clean
and used `git reset --hard` to recover from some errors. Current Go Braid scopes
its preflight checks and automatic commits to the Braid-owned paths for the
command.

For `add`, `pull`, and `remove`, unrelated staged, unstaged, and untracked work
outside `.braids.json` and the selected mirror paths is preserved and is not
included in Braid's automatic commit. These commands accept `--no-commit` to
stage `.braids.json` and selected mirror paths without creating the automatic
commit. `upgrade-config --no-commit` similarly stages only `.braids.json`. The
paths owned by each command still have to be clean unless the command explicitly
supports a different flow. Ignored files under selected mirrors do not block
`pull`, `sync`, or `remove`; pull and sync preserve them, while remove leaves
them on disk after deleting tracked mirror content.

Migration impact:

- You can run Braid with unrelated work in progress more safely.
- Use `--no-commit` on `add`, `pull`, `remove`, or `upgrade-config` when the
  change belongs in the same downstream commit as other related changes.
- If unrelated files are already staged during `--no-commit`, they remain staged
  and can be included in the next manual commit unless you unstage them.
- Staged, tracked, and ordinary untracked changes in selected mirror paths still
  stop `add`, `pull`, `remove`, and plain `sync`. Ignored files also stop `add`,
  but not `pull`, `remove`, or plain `sync`; `sync --autostash` continues to save
  and restore ignored files with the other selected-path state.
- On pull conflict, unrelated staged files remain staged. Go Braid warns about
  that because a manual `git commit` after conflict resolution could include
  those files unless you unstage them.

### `sync` is new

Ruby Braid has no `sync` command. Current Go Braid adds:

```bash
braid sync [selector...] [--pull-only] [--autostash] [--keep]
```

The default `sync` workflow pushes committed local changes only for
branch-tracking sources configured with `"sync_push": true`, then pulls every
mirror in each selected source to the new upstream revision. Add a new opted-in
source with `braid add <url> <path> --sync-push`. Sources with omitted or false
`sync_push` are pull-only during sync, even when explicitly selected.
`--pull-only` skips the push phase for every source. With no selectors, `sync`
selects all branch and tag sources in lexicographic source-name order and skips
revision-locked sources, matching no-selector `pull` selection. A selector may
be `:source` or one of its mirror paths; aliases are deduplicated.

`--autostash` is path-scoped to selected mirrors. It saves selected mirror-path
tracked changes, untracked files, ignored files, and selected-path index state,
runs the sync, then restores that selected state. It does not push uncommitted
changes; the push phase still uses the mirror content recorded in downstream
`HEAD`.

Migration impact:

- Set `"sync_push": true` on branch sources that should participate in sync's
  push phase; omission is pull-only and requires no config version upgrade.
- Use `sync` for the common "push opted-in local mirror commits upstream, then
  pull the downstream recorded revision" workflow.
- Use `sync --pull-only` as a stricter, scoped pull workflow when you do not
  want any push attempt.
- Tag-tracking and revision-locked sources cannot enable `sync_push`; use
  `braid push <path> --branch <branch>` explicitly.

### `push` is more explicit about committed changes and commit messages

Both tools push mirror changes through an isolated temporary repository. The Go
tool keeps that model but tightens the user-facing behavior:

- `push` uses the mirror content recorded in downstream `HEAD`. Uncommitted
  mirror edits are ignored for push planning; commit them downstream first.
- If there are no committed local mirror changes in `HEAD`, Go Braid prints
  `Braid: No local changes found in downstream HEAD. Stopping.`
- For branch-tracking sources, omitting `--branch` pushes to the tracked branch.
  Tag-tracking and revision-locked sources still require an explicit `--branch`.
- The editor starts with commented provenance guidance when Braid can compute
  downstream commits that touched the mirror since the recorded upstream state.
- `push --message <message>` uses the supplied upstream commit message directly
  without opening the editor or running the generated-message command.
- `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` can optionally run a trusted local POSIX
  shell command to generate a draft upstream commit message. The editor still
  opens for review. This generator is disabled by default. Braid obtains the
  shell from `git var GIT_SHELL_PATH`, including on Git for Windows.

Migration impact:

- Commit downstream mirror edits before `braid push` or `braid sync`.
- Existing editor-based push workflows still apply, but users will see
  additional commented guidance in the commit message buffer.
- Treat `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` as trusted local shell code, not as
  a sandboxed plugin mechanism.

## Command Surface Changes

| Area | Ruby Braid | Current Go Braid | Migration impact |
| --- | --- | --- | --- |
| Commands | `add`, `update`, `remove`, `diff`, `push`, `setup`, `version`, `status`, `upgrade-config` | `pull` is the preferred mirror-update command; `update` and `up` are aliases. `setup` is removed. `sync` and `completion` are new. | Remove `setup` from scripts, prefer `pull`, and use Go Braid's `upgrade-config` only for `.braids.json` version 1 to 2 migration. |
| Help form | `braid help`, `braid add help`, `braid add --help`; the old README also advertised `braid help add`, but the `v1.1.10` gem does not provide command-specific help through that form | `braid help`, `braid add help`, `braid add --help` | Prefer `braid <command> help` or `braid <command> --help`. |
| Verbose flag | Per-command `--verbose`/`-v` | Global `--verbose`/`-v` before the command | Use `braid -v pull ...`, not `braid pull -v ...`. |
| Quiet flag | No global quiet flag | Global `--quiet` before the command; incompatible with `--verbose` | Use `braid --quiet <command> ...` in automation that wants data, warnings, errors, and recovery output without progress or informational chatter. |
| Cache flags | Environment variables only | Global `--no-cache` or `--global-cache-dir <path>` before the command, plus environment variables | Put cache flags before the command name. |
| Bash completion | No documented generated completion command | `braid completion bash` prints a Bash completion script | Load it from shell startup or install it in the Bash completion directory to complete global options, commands, command options, and configured mirror paths. |
| `update --head` | Accepted as an option, then errors with a deprecation message | Unknown flag for `pull` and its aliases | Remove it; use `--branch`, `--tag`, or `--revision` with an explicit mirror path. |
| `update` without path | Updates all configured mirrors through Ruby's all-update flow | `pull` without a selector updates branch/tag sources in lexicographic source-name order, skips revision-locked sources, and reports skipped names; `update` and `up` behave the same way | Locked sources are no longer touched by all-update. |
| Strategy flags with no-path `update` | Accepted by the Ruby parser, with inconsistent all-mirror behavior | Rejected for no-path `pull` and its aliases | Pass a local path when changing branch, tag, or revision. |
| `diff` pass-through | Arguments after `--` are passed to `git diff` | Same | Output formatting is not exact Ruby text parity. |

## Configuration Compatibility

The Go tool supports the named-source `.braids.json` shape with
`config_version: 2`, a top-level `sources` object, and a local-to-upstream
`mirrors` object inside each source. Version 1 JSON config can be grouped and
migrated with `braid upgrade-config`.

Supported source attributes are:

- `url`
- `branch`
- `tag`
- `revision`
- `partial_clone` (optional)
- `mirrors` (required and non-empty)

Important differences:

- Legacy `.braids` YAML/PStore config is not read or upgraded.
- Older `.braids.json` layouts without `config_version` are not upgraded.
- `upgrade-config` does not migrate legacy `.braids` YAML/PStore data.
- Unknown root and source fields fail validation; mirror values must be strings.
- Missing `config_version`, missing `sources`, missing `mirrors`, missing `url`, missing
  `revision`, or a source with both `branch` and `tag` fails validation.
- The current Go code no longer has a special legacy `.braids` diagnostic. A
  project that still has only `.braids` will look like it has no Go-readable
  `.braids.json`.

Migration impact:

- If a project is still on `.braids` or an old `.braids.json` format, run Ruby
  Braid `v1.1.10` `braid upgrade-config` first and commit the resulting
  `.braids.json`.
- Check generated or hand-edited `.braids.json` files for unknown fields. Ruby
  Braid was more tolerant in some old upgrade paths; Go Braid is intentionally
  strict.
- Do not rely on Go Braid to remove or migrate `.braids`.

## Path Handling

The Go tool validates paths earlier and more strictly for cross-platform safety.
Configured local mirror paths and upstream mirror values must use `/`
separators and must not contain parent traversal, empty elements, absolute
paths, Windows drive paths, colon characters, or path elements ending in a space
or dot. Local mirror paths also reject `.git` and Windows reserved basenames.

CLI local path arguments may use `/` or `\`; Braid normalizes them before
lookup or storage. Absolute local path arguments are accepted only when they are
inside the downstream worktree. The upstream side of
`local_path=upstream_path` is a Git path and should use `/`.

Migration impact:

- Some existing Ruby-created configs may be rejected if they contain paths that
  worked on one platform but are unsafe or ambiguous on another.
- Remote names are checked for collisions after Braid's sanitization step. Ruby
  Braid noted this collision risk but did not reject it up front.

## Cache and Remote Behavior

Both tools enable a local mirror cache by default, but the cache layout changed.

Ruby Braid defaults to `~/.braid/cache` and derives cache child paths by
sanitizing the upstream URL. Current Go Braid defaults to repository-local
per-URL bare object caches under `.git/braid/cache` and derives cache child
paths from a SHA-256-derived key covering the normalized upstream URL. Sources
with the same URL share objects while retaining source-scoped refs.

Current controls:

- `BRAID_USE_LOCAL_CACHE=false` or `--no-cache` disables the cache.
- `BRAID_GLOBAL_CACHE_DIR=<path>` or `--global-cache-dir <path>` selects a shared
  full-cache directory.
- `--no-cache` and `--global-cache-dir` cannot be used together.
- `BRAID_LOCAL_CACHE_DIR` and `--cache-dir` have been replaced by
  `BRAID_GLOBAL_CACHE_DIR` and `--global-cache-dir`.

Migration impact:

- Do not depend on the old `~/.braid/cache` layout or old cache path names.
- Repository-local caches are disposable. If one is deleted, Braid can rebuild it
  only while upstream still serves the recorded revisions from `.braids.json`.
  Shallow repository-local caches can also make Git report the downstream
  repository as shallow because Braid source objects are recorded in
  `.git/shallow`.
- Tag-tracking sources work when the Go cache is disabled. Ruby Braid could not retrieve
  tag revisions with the cache disabled.

## Pull and Conflict Handling

Non-conflicting pulls still create Braid commits such as:

```text
Braid: Update source 'example' to 'abcdef0'
```

Conflict handling differs in details. Ruby Braid documented that users should
resolve conflicts and manually run `git commit`, or use `git reset --hard` to
abandon the pull. Current Go Braid prints the exact `git add` and
`git commit -F <MERGE_MSG>` commands for the invocation directory.

Current Go Braid stages the updated `.braids.json`, writes `.git/MERGE_MSG`, and
restores conflict-marker files in the mirror path. It also warns if unrelated
staged changes are present.

Migration impact:

- Follow the printed commands after a Go Braid conflict rather than old
  root-only instructions.
- To abandon a conflicted Go Braid pull while preserving unrelated work,
  restore only the mirror path and `.braids.json` from `HEAD`, then remove
  `.git/MERGE_MSG`.

## Output and Automation

The Go tool does not preserve exact Ruby help text, progress messages, blank
lines, or status banners. Stable automation should rely on exit codes,
`.braids.json`, Git state, and documented command behavior rather than exact
Ruby wording.

Go Braid intentionally restores Ruby-like feedback for operations that may
contact upstream repositories or the local cache. It prints semantic start and
completion progress to `stderr` for cache updates, mirror fetches, pull checks,
mirror updates, upstream pushes, status remote checks, diff remote hydration,
and temporary remote changes. Interactive terminals append `.` about every five
seconds during long-running operations; non-interactive output is line-based.

The stream contract is explicit:

- Command-requested data stays on `stdout`, including `status`, `diff`, `help`,
  and `version`.
- Progress and informational output goes to `stderr`.
- Warnings, errors, conflict recovery instructions, skipped revision-locked
  mirror lists, and push stop messages remain visible under `--quiet`.
- `--quiet` suppresses progress and informational output only; it is global and
  must appear before the command name.

Known output differences include:

- Error messages use `braid: ...` style for Go command failures rather than
  Ruby's `Braid: Error: ...` style.
- `status` prints mirror lines without Ruby's top-level "Listing all mirrors"
  banner.
- `diff` still labels all-mirror output with `Braid: Diffing mirror <path>`, but exact
  separators and paging behavior are not a compatibility target.
- Verbose Git tracing uses Go's deterministic argv representation and is
  incompatible with `--quiet`.

## Migration Checklist

1. Upgrade legacy projects with Ruby Braid first:
   `gem install braid -v 1.1.10` and `braid upgrade-config`.
2. Confirm every developer and CI runner has Git 2.45.0 or newer.
3. Install the Go release binary for the target platform and verify
   `SHA256SUMS`.
4. Remove or update automation that uses Ruby's `setup` or legacy
   `upgrade-config` flows, `update --head`, `braid help <command>`, or
   per-command `--verbose`.
5. Run `braid status` and `braid diff` on a clean checkout and compare the
   result with expected mirror state.
6. Review `.braids.json` for unknown fields, unsafe paths, and remote-name
   collisions.
7. Decide whether repository-local caches are acceptable; set
   `BRAID_GLOBAL_CACHE_DIR` or use `--global-cache-dir` if you need a controlled
   shared full-cache path.
8. For push workflows, make sure local mirror edits are committed downstream
   before `braid push` or `braid sync`.
9. Enable `sync_push` only for branch sources that should be pushed, then use
   `braid sync` for the push-then-pull workflow.
10. Optionally install Bash completion with `braid completion bash`.
