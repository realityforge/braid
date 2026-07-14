# Braid

Braid is a simple tool to help track vendor branches in a [Git](http://git-scm.com/)
repository. Braid mirrors selected files or directories from upstream Git repositories
into a downstream repository.

This is a port of the [Ruby Braid](https://github.com/cristibalan/braid) implemented in
Go and built with Bazel.

For migration-impacting differences from Ruby Braid, see
[`docs/migration-from-ruby-braid.md`](docs/migration-from-ruby-braid.md).

## Motivation

Vendoring allows you take the source code of an external library and ensure it's
version controlled along with the main project. This is in contrast to including
a reference to a packaged version of an external library that is available in a
binary artifact repository such as Maven Central, RubyGems or NPM.

Vendoring is useful when you need to patch or customize the external libraries
or the external library is expected to co-evolve with the main project. The
developer can make changes to the main project and patch the library in a single
commit.

The problem arises when the external library makes changes that you want to
integrate into your local vendored version or the developer makes changes to the
local version that they want integrated into the external library.

A typical "implementation" of vendoring is to simply download or checkout the
source for the external library, remove the `.git` or `.svn` directories and
commit it to the main source tree. However this approach makes it very difficult
to update the library. When you want to update the library do you re-apply your
local changes onto a new copy of the vendored library or do you re-apply the
changes from the external library to local version? Both cases involve manual
generation and application of patch files to source trees.

This is where Braid comes into play. Braid makes it easy to vendor in remote git
repositories and use an automated mechanism for updating the external library
and generating patches to upgrade the external library.

Braid creates a `.braids.json` file in the repository root. It records named
upstream sources and the local mirrors materialized from each source. The
configuration controls:

* whether a source is locked to a revision or tracks a tag or branch;
* which upstream files or directories appear at each local mirror path;
* whether partial clone avoids unrelated blobs while hydrating the source.

## Install

For a release build, download the artifact for your platform from the release
page, verify `SHA256SUMS`, make the binary executable on Unix-like systems, and
place it on `PATH`.

```bash
shasum -a 256 -c SHA256SUMS
chmod +x braid-linux-amd64
sudo install -m 0755 braid-linux-amd64 /usr/local/bin/braid
```

For local development, run the binary through Bazel:

```bash
bazel run //cmd/braid:braid -- version
```

## Usage

Braid commands run from any directory inside a Git working tree. Commands that
create automatic commits (`add`, `pull`, and `remove`) require their
command-owned paths to be clean, block unresolved Git operations, and leave
unrelated staged, unstaged, and untracked work untouched and out of Braid's
commits. `status`, `diff`, and `push` are the usual commands for deciding
whether to pull, prepare a patch, or send local mirror changes upstream.

Use `--no-commit` with `add`, `pull`, `remove`, or `upgrade-config` to stage Braid's changes
without creating the automatic commit. Braid stages only `.braids.json` and the
selected mirror paths; unrelated staged files stay staged and will be included
in your next `git commit` unless you unstage them first.

- [Command form](#command-form)
- [Shell completion](#shell-completion)
- [Output and quiet mode](#output-and-quiet-mode)
- [Quick start](#quick-start)
- [Adding sources and mirrors](#adding-sources-and-mirrors)
- [Checking status and local changes](#checking-status-and-local-changes)
- [Pulling mirrors](#pulling-mirrors)
- [Syncing mirrors](#syncing-mirrors)
- [Pushing local changes upstream](#pushing-local-changes-upstream)
- [Removing mirrors](#removing-mirrors)
- [Remotes, cache, and paths](#remotes-cache-and-paths)
- [Command reference](#command-reference)

### Command Form

Global flags must appear before the command name:

```bash
braid [--verbose|-v | --quiet] [--no-cache | --global-cache-dir <path>] <command> [options]
```

`--verbose` prints Git command tracing. `--quiet` suppresses progress and other
informational output, and cannot be used with `--verbose`.

Use the built-in help for the command list or command-specific syntax:

```bash
braid help
braid add help
braid add --help
```

For an upstream with large blobs, opt into Git partial clone with
`braid add <url> <local_path>=<upstream_path> --partial-clone`. This stores
`"partial_clone": true` on the source in config version 2 and
uses Git's `blob:none` filter for repository-local cache hydration and fetches.
The upstream server must support Git object filtering. The setting is ignored
when caching is disabled or a global cache is selected.

Repositories with config version 1 must run `braid upgrade-config`. The command
commits the version 2 config by default; pass `--no-commit` to stage it instead.

### Shell Completion

Braid can print a Bash completion script to `stdout`:

```bash
eval "$(braid completion bash)"
```

To install it persistently, write that output to the location loaded by your
Bash completion setup, for example:

```bash
braid completion bash > /usr/local/etc/bash_completion.d/braid
```

The Bash completion covers global options, commands, command options, source
selectors, and configured mirror paths. Mirror path candidates are printed relative to the
directory where completion is invoked, matching how Braid resolves path
arguments.

### Output And Quiet Mode

Commands that contact an upstream repository or update the local cache print
start and completion messages on `stderr`, for example fetching a source,
updating the cache, checking whether a source is current, pushing upstream, or
setting up Braid-managed remotes. In an interactive terminal, long-running
operations append `.` about every five seconds and print the completion message
on a new line. Non-interactive output is line-based and does not print dots.

Command-requested data stays on `stdout` and is not suppressed by `--quiet`.
This includes `status`, `diff`, `help`, and `version` output. Warnings, errors,
and recovery or result messages also remain visible under `--quiet`; examples
include push provenance warnings, pull conflict instructions, skipped
revision-locked source lists, and push stop messages such as "Source is not up
to date."

### Quick Start

Start anywhere inside an existing Git repository. Add a named upstream source
and one local mirror:

```bash
braid add <upstream-git-url> lib/grit --sync-push
```

Braid copies the upstream content into `lib/grit`, records the source and mirror
in `.braids.json`, and creates a `Braid: Add source ...` commit. If you do not
specify `--branch`, `--tag`, or `--revision`, Braid tracks the upstream
repository's default branch.

Use `--no-commit` when the mirror add belongs in the same commit as other
changes:

```bash
braid add <upstream-git-url> lib/grit --sync-push --no-commit
git commit
```

Later, bring in upstream changes with:

```bash
braid pull lib/grit
```

If you changed the vendored code locally and want to send those changes back
upstream, inspect or save a patch:

```bash
braid diff lib/grit
braid diff lib/grit > grit.patch
```

If you have push access, commit your downstream changes first, then push the
mirror changes back to the tracked branch or to a separate upstream branch:

```bash
braid push lib/grit
braid push lib/grit --branch myproject_customizations
```

Because the source was added with `--sync-push`, `sync` combines the
tracked-branch push and follow-up pull:

```bash
braid sync lib/grit
```

For sources with `"sync_push": true`, it pushes committed local mirror changes
when the branch is still up to date. It then pulls every selected source,
including sources that are not opted into sync pushes, so `.braids.json`
records the new upstream revision. Use the explicit `push` and `pull` commands
when you need to push to a different branch or handle each step separately.

```bash
braid pull lib/grit
```

### Adding Sources And Mirrors

Add a whole upstream repository:

```bash
braid add https://github.com/rails/rails.git vendor/rails
```

Add several local mirrors from one source snapshot. Omit `=upstream_path` to
mirror the repository root:

```bash
braid add https://github.com/twbs/bootstrap.git --name bootstrap \
  vendor/assets/bootstrap=dist \
  licenses/BOOTSTRAP-LICENSE.txt=LICENSE
```

Track a specific branch or tag:

```bash
braid add https://github.com/rails/rails.git vendor/rails --branch 5-0-stable
braid add https://github.com/rails/rails.git vendor/rails-7 --tag v7.0.0
```

Opt a branch-tracking source into the push phase of `braid sync`:

```bash
braid add https://github.com/rails/rails.git vendor/rails --sync-push
```

Without `--sync-push`, `braid sync` pulls the source but does not push it.
`--sync-push` cannot be combined with `--tag` or `--revision`, and cannot be
used when adding mirrors to an existing `:source`. Explicit `braid push` is
unaffected by this setting.

Lock a source to an explicit revision when you do not want ordinary
`braid pull` runs to move it:

```bash
braid add https://github.com/rails/rails.git vendor/rails --revision 5850a65
```

The source name is optional and normally derived from the URL basename. With no
mirror arguments, Braid creates one root mirror at that name. Add mirrors later
from the recorded source revision with:

```bash
braid add :bootstrap docs/bootstrap=docs
```

Each mirror argument is `local_path[=upstream_path]`; no `=` means the upstream
root. Local paths cannot contain `=`.

Before adding, Braid checks any existing `.braids.json` and requires the target
path to be available. Tracked, staged, unstaged, or untracked content at the
target, under the target, or at a blocking ancestor stops the add before Braid
fetches or writes mirror content.

With `--no-commit`, Braid leaves `HEAD` unchanged and stages `.braids.json` plus
the new mirror path. If unrelated files were already staged, Braid warns because
they will be included in a normal `git commit`.

### Checking Status And Local Changes

Show every configured mirror, one mirror, or every mirror for a source:

```bash
braid status
braid status vendor/rails
braid status :rails
```

Status output includes the recorded revision and tracking mode, such as
`[BRANCH=main]`, `[TAG=v1.0]`, or `[REVISION LOCKED]`. It prints two states as
`(Content State, Source State)`. Content states include `Up To Date`, `Modified
Locally`, `Modified Remotely`, `Removed Locally`, `Removed Remotely`, and
`Modified Locally And Remotely`. Source state is `Current`, `Behind`, or
`Locked`, so a source can be behind while one mirror's content is unchanged.

Show local changes as a Git diff:

```bash
braid diff
braid diff --sync-push-only
braid diff vendor/rails
braid diff :rails
braid diff vendor/rails -- --stat
braid diff vendor/rails -- --cached
```

A mirror-path selector diffs only that local mirror. A `:source` selector diffs
every mirror belonging to the source.

`--sync-push-only` limits the command to sources configured with
`"sync_push": true`. Ineligible sources are skipped quietly, including when
explicitly selected. The option filters sources only; each selected mirror
retains the normal `braid diff` comparison behavior.

Arguments after `--` are passed to `git diff`. This is useful for generating
patches, limiting output, or checking staged changes only.

### Pulling Mirrors

Pull one source to the newest revision for its tracked branch or tag. A mirror
path is an alias for its whole source:

```bash
braid pull vendor/rails
```

Pull every eligible source in lexicographic source-name order:

```bash
braid pull
```

Revision-locked sources are skipped by `braid pull` without a selector. When any
are skipped, a successful no-path pull prints:

```text
Braid: skipped revision-locked sources:
  :a
  :z
```

Explicit pulls do not print this skipped-source note. Strategy changes require
a source name or mirror path:

```bash
braid pull vendor/rails --revision <revision>
braid pull vendor/rails --branch main
braid pull vendor/rails --tag <tag>
```

Before pulling, Braid requires `.braids.json` and every mirror path in the source
to be clean in both the index and working tree. For `braid pull` without a
selector, that scoped cleanliness check covers every mirror of every eligible
source before any source is fetched or updated.

Use `--no-commit` to stage a pull without creating the automatic Braid commit:

```bash
braid pull vendor/rails --no-commit
braid pull --no-commit
git commit
```

For no-path `braid pull --no-commit`, eligible sources are processed in source
name order. Each source resolves one revision and updates all its mirrors as one
aggregate tree merge and one staged transaction.

If a pull conflicts with local mirror changes, Braid leaves conflict markers
in the mirror working tree, stages the updated `.braids.json`, and writes a
prepared `MERGE_MSG`. Resolve the conflicts, then run the `git add` and
`git commit` commands printed by Braid from the same directory where you invoked
`braid pull`. They use this shape:

```bash
git add -- ':(top)vendor/rails' ':(top)licenses/RAILS-LICENSE' ':(top).braids.json'
git commit -F '<MERGE_MSG path printed by Braid>'
```

If unrelated files were staged before the conflicted pull, they remain staged
and may be included in that manual commit unless you unstage them first. To
abandon a conflicted pull while preserving unrelated work, restore only the
mirror path and `.braids.json` from `HEAD`, then remove `.git/MERGE_MSG`.

### Syncing Mirrors

`braid sync` pulls selected sources and first pushes committed changes for
branch-tracking sources that have opted in with `sync_push`:

```bash
braid sync vendor/rails
braid sync vendor/rails vendor/rack
```

With no selectors, `braid sync` selects every configured branch or tag source in
lexicographic source-name order and skips revision-locked sources, matching
no-selector `braid pull`. When any revision-locked sources are skipped, successful
no-path `braid sync` and `braid sync --pull-only` runs print:

```text
Braid: skipped revision-locked sources:
  :a
  :z
```

Explicit selectors may be `:source` names or mirror paths. Aliases for the same
source are deduplicated, selected sources run in source-name order, and explicit
selection does not print the skipped-source note.

Before any fetch, push, editor, worktree write, config write, or pull commit,
`sync` checks unresolved Git operation state, `.braids.json`, and every selected
mirror path for index and working tree changes. Dirty mirrors outside an
explicit selection do not block that explicit sync. Ignored files under a
selected mirror block unless `--autostash` preserves them.

Use `--autostash` when selected mirror paths have uncommitted work that should
be carried across the sync:

```bash
braid sync --autostash vendor/rails
braid sync --pull-only --autostash vendor/rails
```

Autostash is path-scoped to the selected mirrors. It saves tracked staged
changes, tracked unstaged changes, tracked deletions, untracked files, and
ignored files under those selected mirror paths, then restores them after sync.
Selected mirror-path index state is restored from the saved stash entry, while
unrelated staged and unstaged files outside selected mirrors are left alone.
Dirty `.braids.json` and unresolved Git operation state still stop before any
autostash is created.

Autostash does not push uncommitted mirror changes. The push phase still uses
only the mirror content recorded in downstream `HEAD`. When sync pushes a
changed branch source, each upstream push uses the same commit-message review
flow described for `braid push`, including optional generated-message prompts
when configured.

If sync reaches a Braid pull conflict after creating an autostash, Braid
leaves the stash intact instead of applying it over conflict markers. Resolve
the Braid pull first using the printed conflict instructions, then follow the
printed recovery command to apply the saved stash and restore selected-path
index state. If automatic restoration succeeds but the saved stash cannot be
dropped safely, Braid leaves your restored work in place, keeps the stash
recoverable, and tells you to inspect `git stash list` before manual cleanup.

The push phase only considers branch-tracking sources with `"sync_push": true`.
Sources with omitted or false `sync_push` are skipped quietly during that phase
and still update normally, even when explicitly selected. Opted-in branch
sources without committed local changes are also skipped quietly. Tag and
revision-locked sources cannot enable `sync_push`; use
`braid push <path> --branch <branch>` for explicit push intent.

If a changed branch source's upstream branch moved since the recorded revision,
`sync` fails before any mirror is pushed. Pull first, resolve conflicts if
needed, commit, then rerun `braid sync`. If the selected mirror path itself was
deleted from downstream `HEAD`, `sync` also fails rather than trying to push the
deletion of the mirror root; deletions inside an existing mirror directory are
ordinary local mirror changes.

`sync` pushes sources sequentially. If an earlier source push succeeds and a
later source's generator or commit editor fails, the earlier upstream commit may
already exist and the pull phase is skipped. Rerun `braid sync` after resolving
the failure to record downstream source revisions.

Use `--pull-only` to run only the pull phase with the same scoped precheck:

```bash
braid sync --pull-only
braid sync --pull-only vendor/rails
```

Use `--keep` to retain temporary Braid remotes used during sync planning, push,
and pull:

```bash
braid sync vendor/rails --keep
```

### Pushing Local Changes Upstream

`braid push` reconstructs one upstream tree from every mirror in the selected
source as recorded in downstream `HEAD`, opens Git's commit editor for one
upstream commit message, and pushes that commit.

For non-interactive pushes, pass the upstream commit message explicitly:

```bash
braid push vendor/rails --message "Apply local Rails customizations"
```

When `--message` (or `-m`) is present, Braid uses that message directly and does
not open the editor or run `BRAID_PUSH_COMMIT_MESSAGE_COMMAND`.

When available, the editor starts with commented guidance listing downstream
commits that touched the mirror path since the last clean mirror state. The
guidance includes full downstream commit messages so you can summarize, copy, or
ignore the relevant context while writing the upstream commit message. Braid
does not generate an upstream subject for you, and leaving the guidance in place
does not add it to the final commit message.

This guidance is best-effort. If Braid cannot compute it safely, or if
`core.commentChar` is set to `auto`, Braid prints a warning and opens the editor
without the provenance block; the push still proceeds through the normal commit
and push checks.

To prefill the editor with a generated draft message, set
`BRAID_PUSH_COMMIT_MESSAGE_COMMAND` to a trusted local POSIX shell command. Empty
or unset disables generation. Braid substitutes these placeholders with
shell-quoted paths and leaves unknown placeholder-like text unchanged:

- `{REPO_DIR}`: downstream repository root.
- `{CONTEXT_DIR}`: temporary prompt context directory.
- `{PROMPT_FILE}`: generated prompt file.
- `{MESSAGE_FILE}`: file where the command must write the proposed message.

Example:

```bash
BRAID_PUSH_COMMIT_MESSAGE_COMMAND='codex exec -C {REPO_DIR} --add-dir {CONTEXT_DIR} --model gpt-5.5 -c '\''model_reasoning_effort="low"'\'' -o {MESSAGE_FILE} < {PROMPT_FILE}'
```

The command runs as `/bin/sh -c` from the downstream repository root with the
current process environment. The prompt includes mirror metadata, downstream
commit provenance when it can be collected, and the staged upstream diff. Diffs
up to 5 KiB are included inline; larger diffs are written under
`{CONTEXT_DIR}` and referenced from the prompt. The configured command is
trusted local shell code and is not sandboxed by Braid. On Windows, configured
generation is not supported; leave the environment variable unset or empty to
use the normal editor flow.

When generation succeeds, Git's editor opens with the generated message followed
by commented provenance guidance when available. The editor-reviewed content is
the message Braid commits. If the generator exits nonzero, does not create the
message file, or writes only whitespace, Braid opens the normal editor template
with commented diagnostics and provenance guidance when available. Those
comments are stripped from the final commit message if left in place.

For branch-tracking sources, pushing without `--branch` targets the tracked branch:

```bash
braid push vendor/rails
```

Use `--branch` to push to a different upstream branch, or when the source tracks
a tag or fixed revision:

```bash
braid push vendor/rails --branch myproject_customizations
```

Braid stops without pushing if the destination branch differs from the recorded
source revision. In that case, pull the source first, resolve any conflicts,
and then push.

### Removing Mirrors

Remove a mirror from the downstream repository:

```bash
braid remove vendor/rails
```

Braid removes that local mirror while retaining other mirrors in its source.
Removing the last mirror removes the source. Remove a source and all its local
mirrors explicitly with:

```bash
braid remove :rails
```

The automatic commit identifies either the removed mirror and source or the
removed source.

Use `--no-commit` to stage the removal without committing:

```bash
braid remove vendor/rails --no-commit
git commit
```

Before removing, Braid requires `.braids.json` and the mirror path to be clean in
both the index and working tree. Local edits, local deletions, staged mirror
changes, and untracked files under the mirror path stop the remove.

`braid remove --keep --no-commit` keeps the Braid-managed Git remote, but still
stages the mirror content removal and `.braids.json` update.

### Remotes, Cache, And Paths

Braid creates Git remotes as needed and removes them when the command finishes.

The local cache is enabled by default. Without overrides, Braid stores one
repository-local bare object cache per upstream URL under `.git/braid/cache`.
These caches
are implementation state and can be rebuilt by `braid add`, `braid status`,
`braid pull`, `braid diff`, `braid push`, or `braid sync` while
the upstream still serves the recorded revisions from `.braids.json`.

Repository-local caches are shallow for common branch, tag, and full-SHA
revision workflows. Fetching from a shallow cache can make the downstream Git
repository report as shallow because Git records shallow source objects in
`.git/shallow`; those commits are Braid source objects, not the downstream
branch history. If an upstream has removed a recorded revision and the
repository-local cache was deleted, Braid fails instead of guessing a base.

Use `BRAID_GLOBAL_CACHE_DIR` or `--global-cache-dir` to choose a shared full-cache
location, and use `BRAID_USE_LOCAL_CACHE=false` or `--no-cache` to disable
caching.

The old `BRAID_LOCAL_CACHE_DIR` environment variable and `--cache-dir` flag have
been replaced by `BRAID_GLOBAL_CACHE_DIR` and `--global-cache-dir`.

Local mirror paths stored in `.braids.json` always use repo-root-relative `/`
separators, and ordinary Braid output uses those same repo-root-relative paths.
CLI mirror path arguments may use `/` or `\`; Braid resolves them relative to
the directory where the command was invoked, then normalizes them before config
lookup or storage. Absolute `local_path` inputs are accepted only when they are
inside the Git working tree, and stored config paths remain relative.

Commands without a `local_path`, such as `braid status`, `braid diff`,
`braid pull`, and `braid sync`, operate on the repository-wide
mirror set from any subdirectory. Relative `--global-cache-dir` values and
`BRAID_GLOBAL_CACHE_DIR` values remain relative to the process directory. Git
diff arguments after `braid diff ... --` are passed through as raw `git diff`
arguments from the process directory; Braid only anchors its own internal mirror
pathspecs.

Upstream paths in `local_path=upstream_path` use Git's `/` separator. When
adding from a local Windows repository path such as `C:\src\upstream.git`,
Braid preserves the fetch target and derives the default source/mirror name from
the repository basename.

Config version 2 records named sources and their mirrors:

```json
{
  "config_version": 2,
  "sources": {
    "replicant": {
      "url": "https://github.com/replicant4j/replicant.git",
      "branch": "master",
      "revision": "18480c9dc34f948218a0c15370712d27b2626fa0",
      "sync_push": true,
      "mirrors": {
        "licenses/replicant-LICENSE.txt": "LICENSE.txt",
        "vendor/libs/replicant": ""
      }
    }
  }
}
```

Braid validates configured mirror paths for cross-platform safety. It does not
preflight every file inside the selected upstream tree; if an upstream filename
cannot be materialized on the current OS, Git reports the checkout failure.
The optional `sync_push` field defaults to false and is only valid for
branch-tracking sources.

When you vendor only a subdirectory or single file, remember that license files
outside the mirrored path are not copied automatically. If the upstream license
must travel with the vendored content, add it separately or mirror a path that
includes it.

### Command Reference

| Command | Purpose |
| --- | --- |
| `add [--no-commit]` | Add a source with mirrors, or add mirrors to an existing `:source`. |
| `status` | Show whether mirrors have remote, local, or removal changes. |
| `diff [--sync-push-only]` | Show local mirror changes, optionally limited to sources that participate in sync pushes, with Git diff arguments after `--`. |
| `pull [--no-commit]` | Pull one source atomically, or every eligible source. |
| `push` | Push a source's committed mirror changes upstream as one commit. |
| `sync [selector...] [--pull-only] [--autostash] [--keep]` | Push then pull selected sources. |
| `remove [--no-commit]` | Remove a mirror by path or a source by `:name`. |
| `version` | Print the Braid version. |
| `completion bash` | Print the Bash completion script. |

## Build And Test

This repository is Bazel-first. Use Bazel as the source of truth for builds,
tests, formatting, vetting, linting, and Go toolchain selection.

```bash
bazel test --test_env=PATH //...
bazel build //cmd/braid:braid
```

Fast Go quality checks used by CI run through the Bazel-pinned Go SDK:

```bash
bazel run @rules_go//go -- fmt ./...
bazel test --test_env=PATH //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
```

## GitHub Actions CI Workflow

GitHub Actions CI workflow lives in `.github/workflows/ci.yml` and has two job families:

- `Go quality and lint` runs formatting, tests, vet, and golangci-lint through
  Bazel. Tests use `bazel test --test_env=PATH //...` so unit tests, real-Git
  tests, and the executable integration target all run as first-class Bazel
  targets.
- `Integration (<platform>)` runs the executable integration target on the
  non-default native release platforms used for early cross-platform signal.

Each job installs Bazel, then uses `rules_go` to supply Go. golangci-lint is run
with `bazel run @rules_go//go -- run ...` so CI still has a single automation
entrypoint: Bazel.

# Release Builds

Releases are cut through GitHub Actions:

1. Run the `Release Cut` workflow from `main` with a stable version such as
   `0.1.0` or `v0.1.0`.
2. The workflow validates the exact `main` commit, creates an annotated
   `vX.Y.Z` tag, and dispatches the `Release` workflow on that tag.
3. The `Release` workflow builds the supported native artifacts, verifies
   checksums, and creates a draft GitHub release.
4. Review the draft release assets and generated notes, then publish manually.

The workflow files own the operational details, including runner labels,
permissions, version stamping, artifact names, and verification commands.
