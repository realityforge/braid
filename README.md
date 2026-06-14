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

Global cache flags must appear before the command:

```bash
braid [--no-cache | --cache-dir <path>] <command> [options]
```

Common flows:

```bash
braid add https://github.com/example/upstream.git vendor/upstream --branch main
braid add https://github.com/example/upstream.git vendor/lib --path lib
braid status
braid diff vendor/upstream -- --stat
braid update vendor/upstream
braid update
braid push vendor/upstream --branch main
braid remove vendor/upstream
```

Commands:

- `add`: add a branch, tag, or revision mirror.
- `setup`: add or refresh Braid-managed Git remotes.
- `status`: show local and upstream mirror status.
- `diff`: show local mirror changes, with Git diff arguments after `--`.
- `update`: update one mirror, or every branch/tag mirror when no path is given.
- `push`: push local mirror changes upstream.
- `remove`: remove mirrored content and config.
- `version`: print the Braid version.

Mirror paths stored in `.braids.json` always use `/` separators. CLI mirror
path arguments may use `/` or `\`; Braid normalizes them before config lookup.
The `--path` option is an upstream Git path and should use Git's `/`
separator. When adding from a local Windows repository path such as
`C:\src\upstream.git`, Braid keeps that original upstream value for Git and
derives the default mirror path from the repository basename.

The local cache is enabled by default. Without overrides, Braid stores it under
the OS user cache directory with a `braid` child directory. Use
`BRAID_LOCAL_CACHE_DIR` or `--cache-dir` to choose a location, and use
`BRAID_USE_LOCAL_CACHE=false` or `--no-cache` to disable it.

Braid validates configured mirror paths for cross-platform safety. It does not
preflight every file inside the selected upstream tree; if an upstream filename
cannot be materialized on the current OS, Git reports the checkout failure.

## Build And Test

The build infrastructure relies on Bazel or Bazelisk with Bzlmod enabled.

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

Release target builds:

```bash
bazel build --platforms=@rules_go//go/toolchain:linux_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:linux_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid
```

The full release artifact process, checksum generation, manual macOS
signing/notarization path, and native smoke matrix are documented in
[`docs/release.md`](docs/release.md).

## Documentation

- [`docs/migration.md`](docs/migration.md): migration notes and intentional
  divergences from Ruby Braid.
- [`docs/contributing.md`](docs/contributing.md): contributor workflow, test
  strategy, and Git assumptions.
- [`docs/ci.md`](docs/ci.md): GitHub Actions workflow and local Go quality
  checks.
- [`docs/future-subdirectory-execution.md`](docs/future-subdirectory-execution.md):
  why v1 requires root-only execution.
