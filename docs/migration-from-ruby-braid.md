# User-Facing Changes From Ruby Braid

This document compares the current Go/Bazel Braid tool with the original Ruby
Braid tool from <https://github.com/cristibalan/braid>. It is for users deciding
whether to migrate and for maintainers checking what behavior might change.

Comparison baseline:

- Original Ruby Braid: `cristibalan/braid` `v1.1.10`
  (`16729390a2a8e6b45919545b056a1a7ac83c14d6`).
- Current Go Braid: `realityforge/braid` `v0.9.4`
  (`43a029999aa3b4af8e66f34d9a6d4c6c9ade06b1`).
- Initial Go-port migration notes:
  <https://github.com/realityforge/braid/blob/d024c502c5eb988f47ac54944f0c5be16bd3b045/docs/migration.md>.

The Go tool keeps the core modern Braid model: mirrors are recorded in
`.braids.json`, mirror content is copied into the downstream repository, and the
main workflows are `add`, `status`, `diff`, `pull`, `push`, `setup`, and
`remove`. `pull` is the documented spelling for updating mirror content;
`update` and `up` are accepted aliases. The differences below are the parts most
likely to matter in day to day use or automation.

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
`braid diff`, `braid pull`, `braid sync`, and `braid setup`, still operate on
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

For `add`, `pull`, and `remove`, unrelated staged, unstaged, and untracked
work outside `.braids.json` and the selected mirror paths is preserved and is not
included in Braid's automatic commit. The selected mirror paths and
`.braids.json` still have to be clean unless the command explicitly supports a
different flow.

Migration impact:

- You can run Braid with unrelated work in progress more safely.
- Dirty selected mirror paths still stop `add`, `pull`, `remove`, and `sync`
  unless `sync --autostash` is used.
- On pull conflict, unrelated staged files remain staged. Go Braid warns about
  that because a manual `git commit` after conflict resolution could include
  those files unless you unstage them.

### `sync` is new

Ruby Braid has no `sync` command. Current Go Braid adds:

```bash
braid sync [local_path...] [--pull-only] [--autostash] [--keep]
```

The default `sync` workflow pushes committed local changes for branch-tracking
mirrors and then pulls the downstream mirror to the new upstream revision.
`--pull-only` skips the push phase and only pulls. With no paths, `sync` selects
all branch and tag mirrors in lexicographic path order and skips revision-locked
mirrors, matching no-path `pull` selection.

`--autostash` is path-scoped to selected mirrors. It saves selected mirror-path
tracked changes, untracked files, ignored files, and selected-path index state,
runs the sync, then restores that selected state. It does not push uncommitted
changes; the push phase still uses the mirror content recorded in downstream
`HEAD`.

Migration impact:

- Use `sync` for the common "push local mirror commits upstream, then pull the
  downstream recorded revision" workflow.
- Use `sync --pull-only` as a stricter, scoped pull workflow when you do not
  want any push attempt.
- Do not expect `sync` to push tag or revision mirrors with local changes unless
  you use `braid push <path> --branch <branch>` explicitly.

### `push` is more explicit about committed changes and commit messages

Both tools push mirror changes through an isolated temporary repository. The Go
tool keeps that model but tightens the user-facing behavior:

- `push` uses the mirror content recorded in downstream `HEAD`. Uncommitted
  mirror edits are ignored for push planning; commit them downstream first.
- If there are no committed local mirror changes in `HEAD`, Go Braid prints
  `Braid: No local changes found in downstream HEAD. Stopping.`
- For branch mirrors, omitting `--branch` pushes to the tracked branch. Tag and
  revision mirrors still require an explicit `--branch`.
- The editor starts with commented provenance guidance when Braid can compute
  downstream commits that touched the mirror since the recorded upstream state.
- `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` can optionally run a trusted local POSIX
  shell command to generate a draft upstream commit message. The editor still
  opens for review. This generator is disabled by default and is not supported
  on Windows.

Migration impact:

- Commit downstream mirror edits before `braid push` or `braid sync`.
- Existing editor-based push workflows still apply, but users will see
  additional commented guidance in the commit message buffer.
- Treat `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` as trusted local shell code, not as
  a sandboxed plugin mechanism.

## Command Surface Changes

| Area | Ruby Braid | Current Go Braid | Migration impact |
| --- | --- | --- | --- |
| Commands | `add`, `update`, `remove`, `diff`, `push`, `setup`, `version`, `status`, `upgrade-config` | `pull` is the documented mirror-update command; `update` and `up` are aliases; same other core commands, plus `sync`; no `upgrade-config` | Prefer `pull` in new docs and scripts. Scripts using `upgrade-config` must run Ruby Braid before migration or be removed. |
| Help form | `braid help`, `braid add help`, `braid add --help`; the old README also advertised `braid help add`, but the `v1.1.10` gem does not provide command-specific help through that form | `braid help`, `braid add help`, `braid add --help` | Prefer `braid <command> help` or `braid <command> --help`. |
| Verbose flag | Per-command `--verbose`/`-v` | Global `--verbose`/`-v` before the command | Use `braid -v pull ...`, not `braid pull -v ...`. |
| Quiet flag | No global quiet flag | Global `--quiet` before the command; incompatible with `--verbose` | Use `braid --quiet <command> ...` in automation that wants data, warnings, errors, and recovery output without progress or informational chatter. |
| Cache flags | Environment variables only | Global `--no-cache` or `--cache-dir <path>` before the command, plus environment variables | Put cache flags before the command name. |
| Bash completion | No documented generated completion command | `braid completion bash` prints a Bash completion script | Load it from shell startup or install it in the Bash completion directory to complete global options, commands, command options, and configured mirror paths. |
| `update --head` | Accepted as an option, then errors with a deprecation message | Unknown flag for `pull` and its aliases | Remove it; use `--branch`, `--tag`, or `--revision` with an explicit mirror path. |
| `update` without path | Updates all configured mirrors through Ruby's all-update flow | `pull` without a path updates branch/tag mirrors in lexicographic path order, skips revision-locked mirrors, and reports skipped paths; `update` and `up` behave the same way | Locked mirrors are no longer touched by all-update. |
| Strategy flags with no-path `update` | Accepted by the Ruby parser, with inconsistent all-mirror behavior | Rejected for no-path `pull` and its aliases | Pass a local path when changing branch, tag, or revision. |
| `diff` pass-through | Arguments after `--` are passed to `git diff` | Same | Output formatting is not exact Ruby text parity. |
| `setup --force` and command `--keep` | Supported | Supported where relevant | Remote cleanup behavior is broadly preserved. |

## Configuration Compatibility

The Go tool supports only the modern `.braids.json` shape with
`config_version: 1` and a `mirrors` object keyed by local mirror path.

Supported mirror attributes remain:

- `url`
- `branch`
- `tag`
- `path`
- `revision`

Important differences:

- Legacy `.braids` YAML/PStore config is not read or upgraded.
- Older `.braids.json` layouts without `config_version` are not upgraded.
- `upgrade-config` is not implemented.
- Unknown root fields and unknown mirror fields fail validation.
- Missing `config_version`, missing `mirrors`, missing `url`, missing
  `revision`, or a mirror with both `branch` and `tag` fails validation.
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
Configured local mirror paths and upstream `path` values must use `/`
separators and must not contain parent traversal, empty elements, absolute
paths, Windows drive paths, colon characters, or path elements ending in a space
or dot. Local mirror paths also reject `.git` and Windows reserved basenames.

CLI local path arguments may use `/` or `\`; Braid normalizes them before
lookup or storage. Absolute local path arguments are accepted only when they are
inside the downstream worktree. The upstream `--path` value is a Git path and
should use `/`.

Migration impact:

- Some existing Ruby-created configs may be rejected if they contain paths that
  worked on one platform but are unsafe or ambiguous on another.
- Remote names are checked for collisions after Braid's sanitization step. Ruby
  Braid noted this collision risk but did not reject it up front.

## Cache and Remote Behavior

Both tools enable a local mirror cache by default, but the cache layout changed.

Ruby Braid defaults to `~/.braid/cache` and derives cache child paths by
sanitizing the upstream URL. Current Go Braid defaults to the operating system
user cache directory with a `braid` child directory and derives cache child paths
from a SHA-256 hash of the upstream URL.

Current controls:

- `BRAID_USE_LOCAL_CACHE=false` or `--no-cache` disables the cache.
- `BRAID_LOCAL_CACHE_DIR=<path>` or `--cache-dir <path>` selects the cache
  directory.
- `--no-cache` and `--cache-dir` cannot be used together.

Migration impact:

- Do not depend on the old `~/.braid/cache` layout or old cache path names.
- Braid-managed remotes created by `setup` may point at different cache paths.
- Tag mirrors work when the Go cache is disabled. Ruby Braid could not retrieve
  tag revisions with the cache disabled.

## Pull and Conflict Handling

Non-conflicting pulls still create Braid commits such as:

```text
Braid: Update mirror 'vendor/example' to 'abcdef0'
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
and setup remote changes. Interactive terminals append `.` about every five
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
- `diff` still labels all-mirror output with `Braid: Diffing <path>`, but exact
  separators and paging behavior are not a compatibility target.
- Verbose Git tracing uses Go's deterministic argv representation and is
  incompatible with `--quiet`.

## Migration Checklist

1. Upgrade legacy projects with Ruby Braid first:
   `gem install braid -v 1.1.10` and `braid upgrade-config`.
2. Confirm every developer and CI runner has Git 2.45.0 or newer.
3. Install the Go release binary for the target platform and verify
   `SHA256SUMS`.
4. Remove or update automation that uses `upgrade-config`, `update --head`,
   `braid help <command>`, or per-command `--verbose`.
5. Run `braid status` and `braid diff` on a clean checkout and compare the
   result with expected mirror state.
6. Review `.braids.json` for unknown fields, unsafe paths, and remote-name
   collisions.
7. Decide whether the new default cache location is acceptable; set
   `BRAID_LOCAL_CACHE_DIR` or use `--cache-dir` if you need a controlled path.
8. For push workflows, make sure local mirror edits are committed downstream
   before `braid push` or `braid sync`.
9. Prefer `braid sync` for the push-then-pull workflow once the team has
   tested it on the repository.
10. Optionally install Bash completion with `braid completion bash`.
