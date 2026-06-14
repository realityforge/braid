# Migration Notes

These notes cover moving modern Braid users to the Go/Bazel port. The goal is
behavior parity for current `.braids.json` workflows, not preservation of every
historic Ruby implementation detail.

## Supported Config

The Go port supports `.braids.json` with `config_version: 1` and a `mirrors`
object keyed by local mirror path.

```json
{
  "config_version": 1,
  "mirrors": {
    "vendor/example": {
      "url": "https://github.com/example/upstream.git",
      "branch": "main",
      "path": "lib",
      "revision": "0123456789abcdef"
    }
  }
}
```

Supported mirror selectors:

- `branch`: follow a branch.
- `tag`: track a tag.
- `revision`: pin to an explicit revision.
- `path`: mirror a subdirectory or single file from the upstream repository.

Unknown fields, missing required fields, and unsupported config versions fail
validation. Legacy `.braids` config fails with an unsupported legacy diagnostic.

## Intentional Divergences

- `upgrade-config` is not implemented and is treated as an unknown command.
- Legacy `.braids` YAML/PStore config is not read or upgraded.
- `update --head` is not implemented.
- Commands that operate on a repository must run from the downstream worktree
  root. Subdirectory execution is future work.
- Help text and ordinary diagnostics are idiomatic Go CLI text. Scripts should
  rely on exit codes, config, and Git side effects rather than Ruby wording.
- Local and remote mirror paths are validated earlier and more strictly for
  portable behavior across Linux, macOS, and Windows.
- Product code invokes Git through argument arrays only. It never invokes Git
  through a shell.
- The local mirror cache is enabled by default. `BRAID_USE_LOCAL_CACHE` and
  `BRAID_LOCAL_CACHE_DIR` are supported; pre-command `--no-cache` and
  `--cache-dir <path>` override the environment.

## Update All

`braid update` without a local path updates branch and tag mirrors in config
path order. Revision-locked mirrors are skipped. Strategy flags such as
`--branch`, `--tag`, and `--revision` require a local path.

## Push

`braid push` pushes local mirror changes only when the recorded mirror revision
matches the current upstream revision. A tag or revision mirror requires an
explicit `--branch` target. The temporary push repository copies local
`user.name`, `user.email`, and `commit.gpgsign` settings when present.

## Release Notes

The first Go port release produces raw binaries and checksums. macOS
signing/notarization is documented as a manual path and is not automated in the
initial release workflow. See [`docs/release.md`](release.md).
