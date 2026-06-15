# Compatibility Matrix

Status: completed
Last updated: 2026-06-14

## Compatibility Policy

The Go port should preserve user-visible behavior that matters for modern `.braids.json` users. It should not preserve historic migration paths, Ruby internals, or unsafe/ambiguous behavior where a cleaner Go implementation can reject early.

## Runtime Dependencies

| Dependency | Ruby Braid | Go Port |
|---|---|---|
| Ruby runtime | Required | Not required |
| Go runtime | Not applicable | Not required on target |
| Git executable | Required, minimum 2.8.0 | Required, minimum 2.39.0 |
| Shell | Ruby uses safe process APIs for Git but tests use shell snippets | Product code must not use shell execution |
| `.braids.json` | Supported | Supported |
| `.braids` YAML/PStore | Legacy upgrade path | Unsupported by design |

## Command Compatibility

| Command | Compatibility target | Notes |
|---|---|---|
| `add` | High | Preserve branch/tag/revision/path/single-file behavior. |
| `update` | High | Preserve merge/conflict semantics; Git minimum may simplify internals. |
| `remove` | High | Preserve config/content/remote behavior. |
| `diff` | High | Preserve `--` passthrough and single-file diff behavior. |
| `push` | High | Preserve behavior; implementation details can change only with test evidence. |
| `setup` | High | Preserve remote setup and cache enabled by default. |
| `status` | Medium | Preserve status meaning, not exact Ruby markers. |
| `version` | High | Output should identify Go port version clearly; exact Ruby string not required. |
| `upgrade-config` | None | Intentionally removed; unknown command in Go port. |

## Config Compatibility

| Scenario | Behavior |
|---|---|
| Valid config version 1 with mirrors | Load, validate, operate, write stable JSON. |
| Future config version | Fail with clear "too old to understand" style diagnostic. |
| Missing config | Read-only commands fail where appropriate; no upgrade command exists. |
| Legacy `.braids` exists | Unsupported diagnostic; no upgrade attempt. |
| Unknown mirror attributes | Fail validation rather than silently preserving unknown state. |
| Missing required `url` or `revision` | Fail validation. |
| Both `branch` and `tag` present | Fail validation. |

## Intentional Divergences Register

| ID | Area | Divergence | Rationale | Approval |
|---|---|---|---|---|
| D-01 | Legacy config | No YAML/PStore `.braids` support | User explicitly does not need historic infrastructure. | locked |
| D-02 | Removed mirror types | No SVN/full-history migration | Removed behavior is historic and adds complexity. | locked |
| D-03 | Path validation | Reject unsafe paths earlier than Ruby | Security and cross-platform predictability. | review round 2 resolved |
| D-04 | Git minimum | Requires Git 2.39.0 or newer | Reduce compatibility branches while supporting the default Apple Git on GitHub Actions macOS runners. | Q-02 and Q-09 resolved |
| D-05 | Help/output text | New idiomatic output/help design with behavior parity only | Avoid preserving Ruby CLI framework quirks and brittle text tests. | Q-04 resolved |
| D-06 | Command surface | `upgrade-config` is removed | Historic config migration is out of scope. | Q-03 resolved |
| D-07 | Cache flags and no-cache tag behavior | Cache remains on by default and gains global `--no-cache` and `--cache-dir` overrides; tag and annotated-tag mirrors must still work when cache is disabled | Preserve performance by default while making runtime cache choice explicit and avoiding feature loss when cache is disabled. | Q-05, Q-10, and review round 2 resolved |
| D-08 | Release artifacts | First release provides raw binaries and checksums; signing is documented manual process | Avoid blocking the port on account/secret/notarization setup. | Q-07 resolved |
| D-09 | Worktree location | v1 remains root-only; subdirectory execution is future work | Avoid path normalization risk across every command during the port. | Q-08 resolved |
| D-10 | Deprecated flags | `update --head` is not implemented | The option is unused/deprecated in Ruby and preserving it only adds parser and error compatibility burden. | Review round 1 |
| D-11 | Update all mirrors | `braid update` without a path updates branch/tag mirrors, skips revision-locked mirrors, and rejects strategy flags | Avoid accidental bulk retargeting and make all-mirror behavior predictable. | Review round 3 |

## Future Enhancements

| ID | Enhancement | Trigger |
|---|---|---|
| F-01 | Support running commands from subdirectories of the downstream Git worktree. | Consider after the Go port has stable command parity and cross-platform tests. |

## Native Release Smoke Evidence

Concrete runner labels, artifact names, and smoke commands are documented in
`docs/release.md`.

| Release target | Required before release cut |
|---|---|
| `linux_amd64` | `braid version`; fixture-backed `braid add` in a temp repo. |
| `linux_arm64` | `braid version`; fixture-backed `braid add` in a temp repo. |
| `darwin_amd64` | `braid version`; fixture-backed `braid add` in a temp repo. |
| `darwin_arm64` | `braid version`; fixture-backed `braid add` in a temp repo. |
| `windows_amd64` | `braid version`; fixture-backed `braid add` in a temp repo. |

## Release Target Matrix

Q-01 decision: support the mainstream set first.

| OS | Architecture | Bazel platform | Support level |
|---|---|---|---|
| Linux | amd64 | `@rules_go//go/toolchain:linux_amd64` | release |
| Linux | arm64 | `@rules_go//go/toolchain:linux_arm64` | release |
| macOS | amd64 | `@rules_go//go/toolchain:darwin_amd64` | release |
| macOS | arm64 | `@rules_go//go/toolchain:darwin_arm64` | release |
| Windows | amd64 | `@rules_go//go/toolchain:windows_amd64` | release |

## Validation Mapping

| Compatibility area | Validation |
|---|---|
| Config format | Unit tests, golden `.braids.json`, schema-inspired validation tests |
| Git side effects | Real-Git integration tests |
| Ruby parity | Temporary characterization tests during port only; not part of final required gates |
| Cross-platform paths | Native OS CI plus targeted path validation tests |
| Release builds | Bazel platform builds for approved targets |
