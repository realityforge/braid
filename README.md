# Braid

Braid mirrors selected files or directories from upstream Git repositories into
a downstream repository. This port is implemented in Go and built with Bazel.

## Requirements

- Bazel or Bazelisk with Bzlmod enabled.
- Git 2.43.0 or newer at runtime.
- Repository commands must run from the downstream Git worktree root.

The Go port supports the modern `.braids.json` config format. Legacy `.braids`
YAML/PStore config is intentionally unsupported.

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

## Build And Test

```bash
bazel test //...
bazel build //cmd/braid:braid
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
- [`docs/future-subdirectory-execution.md`](docs/future-subdirectory-execution.md):
  why v1 requires root-only execution.
