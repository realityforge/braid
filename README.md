# Braid

Braid is a simple tool to help track vendor branches in a [Git](http://git-scm.com/)
repository. Braid mirrors selected files or directories from upstream Git repositories
into a downstream repository.

This is a port of the [Ruby Braid](https://github.com/cristibalan/braid) implemented in
Go and built with Bazel.

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

Braid creates a file `.braids.json` in the root of your repository that contains
references to external libraries or mirrors. The configuration allows you to control
aspects of the mirroring process such as;

* whether the mirror is locked to a particular version of the external library.
* whether the mirror is tracking a tag or a branch.
* whether the mirror includes the entire external library or just a subdirectory.

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

Braid commands run from the root of a Git working tree. `add`, `update`, and
`remove` write commits and require a clean working tree. `status`, `diff`, and
`push` are the usual commands for deciding whether to update, prepare a patch, or
send local mirror changes upstream.

- [Command form](#command-form)
- [Quick start](#quick-start)
- [Adding mirrors](#adding-mirrors)
- [Checking status and local changes](#checking-status-and-local-changes)
- [Updating mirrors](#updating-mirrors)
- [Pushing local changes upstream](#pushing-local-changes-upstream)
- [Removing mirrors](#removing-mirrors)
- [Remotes, cache, and paths](#remotes-cache-and-paths)
- [Command reference](#command-reference)

### Command Form

Global flags must appear before the command name:

```bash
braid [--verbose|-v] [--no-cache | --cache-dir <path>] <command> [options]
```

Use the built-in help for the command list or command-specific syntax:

```bash
braid help
braid add help
braid add --help
```

### Quick Start

Start in the root of an existing Git repository with a clean working tree. Add a
mirror at the path where you want the upstream content to live:

```bash
braid add <upstream-git-url> lib/grit
```

Braid copies the upstream content into `lib/grit`, records the mirror in
`.braids.json`, stages both, and creates a `Braid: Add mirror ...` commit. If you
do not specify `--branch`, `--tag`, or `--revision`, Braid tracks the upstream
repository's default branch.

Later, bring in upstream changes with:

```bash
braid update lib/grit
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

After upstream accepts your changes, update the mirror again so your downstream
repository records the accepted upstream revision:

```bash
braid update lib/grit
```

### Adding Mirrors

Add a whole upstream repository:

```bash
braid add https://github.com/rails/rails.git vendor/rails
```

Add only a subdirectory or a single file from upstream:

```bash
braid add https://github.com/twbs/bootstrap.git vendor/assets/bootstrap --path dist
braid add <upstream-git-url> licenses/PROJECT-LICENSE.txt --path LICENSE.txt
```

Track a specific branch or tag:

```bash
braid add https://github.com/rails/rails.git vendor/rails --branch 5-0-stable
braid add https://github.com/rails/rails.git vendor/rails-7 --tag v7.0.0
```

Lock a mirror to an explicit revision when you do not want ordinary
`braid update` runs to move it:

```bash
braid add https://github.com/rails/rails.git vendor/rails --revision 5850a65
```

The `local_path` argument is optional. If you omit it, Braid derives the local
path from the upstream repository name, or from the `--path` basename when
mirroring a subdirectory or file.

### Checking Status And Local Changes

Show every configured mirror, or just one mirror:

```bash
braid status
braid status vendor/rails
```

Status output includes the recorded revision and tracking mode, such as
`[BRANCH=main]`, `[TAG=v1.0]`, or `[REVISION LOCKED]`. It also reports useful
state markers:

- `(Remote Modified)`: the tracked branch or tag points at a different upstream
  revision.
- `(Locally Modified)`: the vendored files differ from the recorded upstream
  revision.
- `(Removed Locally)`: the configured mirror path is no longer present in the
  downstream repository.

Show local changes as a Git diff:

```bash
braid diff
braid diff vendor/rails
braid diff vendor/rails -- --stat
braid diff vendor/rails -- --cached
```

Arguments after `--` are passed to `git diff`. This is useful for generating
patches, limiting output, or checking staged changes only.

### Updating Mirrors

Update one mirror to the newest revision for its tracked branch or tag:

```bash
braid update vendor/rails
```

Update every branch and tag mirror in `.braids.json`:

```bash
braid update
```

Revision-locked mirrors are skipped by `braid update` without a path. Strategy
changes require a local path:

```bash
braid update vendor/rails --revision <revision>
braid update vendor/rails --branch main
braid update vendor/rails --tag <tag>
```

If an update conflicts with local mirror changes, Braid leaves conflict markers
in the working tree and writes a prepared `.git/MERGE_MSG`. Resolve the
conflicts, stage the resolved files and `.braids.json`, then commit. To abandon
the conflicted update, reset the working tree and index with `git reset --hard`.

### Pushing Local Changes Upstream

`braid push` creates an upstream commit from the mirror content recorded in your
downstream `HEAD`, opens Git's commit editor for the upstream commit message, and
pushes that commit.

For branch mirrors, pushing without `--branch` targets the tracked branch:

```bash
braid push vendor/rails
```

Use `--branch` to push to a different upstream branch, or when the mirror tracks
a tag or fixed revision:

```bash
braid push vendor/rails --branch myproject_customizations
```

Braid stops without pushing if the upstream branch has moved since the recorded
mirror revision. In that case, update the mirror first, resolve any conflicts,
and then push.

### Removing Mirrors

Remove a mirror from the downstream repository:

```bash
braid remove vendor/rails
```

Braid removes the vendored content, updates `.braids.json`, and creates a
`Braid: Remove mirror ...` commit.

### Remotes, Cache, And Paths

Braid normally creates Git remotes as needed and removes them when the command
finishes. Use `setup` when you want Braid-managed remotes to exist in the
repository, for example before inspecting them with Git:

```bash
braid setup
braid setup vendor/rails --force
```

The local cache is enabled by default. Without overrides, Braid stores it under
the OS user cache directory with a `braid` child directory. Use
`BRAID_LOCAL_CACHE_DIR` or `--cache-dir` to choose a location, and use
`BRAID_USE_LOCAL_CACHE=false` or `--no-cache` to disable it.

Mirror paths stored in `.braids.json` always use `/` separators. CLI mirror path
arguments may use `/` or `\`; Braid normalizes them before config lookup. The
`--path` option is an upstream Git path and should use Git's `/` separator. When
adding from a local Windows repository path such as `C:\src\upstream.git`, Braid
keeps that original upstream value for Git and derives the default mirror path
from the repository basename.

Braid validates configured mirror paths for cross-platform safety. It does not
preflight every file inside the selected upstream tree; if an upstream filename
cannot be materialized on the current OS, Git reports the checkout failure.

When you vendor only a subdirectory or single file, remember that license files
outside the mirrored path are not copied automatically. If the upstream license
must travel with the vendored content, add it separately or mirror a path that
includes it.

### Command Reference

| Command | Purpose |
| --- | --- |
| `add` | Add a branch, tag, or revision mirror and create the initial Braid commit. |
| `status` | Show whether mirrors have remote, local, or removal changes. |
| `diff` | Show local mirror changes, with Git diff arguments after `--`. |
| `update` | Update one mirror, or every branch/tag mirror when no path is given. |
| `push` | Push committed local mirror changes upstream. |
| `remove` | Remove mirrored content and config. |
| `setup` | Add or refresh Braid-managed Git remotes. |
| `version` | Print the Braid version. |

## Build And Test

This repository is Bazel-first. Use Bazel as the source of truth for builds,
tests, formatting, vetting, linting, and Go toolchain selection.

```bash
bazel test //...
bazel build //cmd/braid:braid
```

Fast Go quality checks used by CI run through the Bazel-pinned Go SDK:

```bash
bazel run @rules_go//go -- fmt ./...
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
```

Release automation and the manual macOS signing/notarization caveat are
documented in [`docs/release.md`](docs/release.md).
GitHub Actions CI and the local pull request checks are documented in
[`docs/ci.md`](docs/ci.md).

## Documentation

- [`docs/migration.md`](docs/migration.md): migration notes and intentional
  divergences from Ruby Braid.
- [`docs/ci.md`](docs/ci.md): GitHub Actions workflow and local Go quality
  checks.
