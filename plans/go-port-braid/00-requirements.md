# Go Port Requirements

Status: completed
Last updated: 2026-06-14

## Mission

Port `vendor/braid` from Ruby to an idiomatic Go command-line tool built with Bazel. The delivered tool should be a standalone executable for supported Linux, macOS, and Windows targets, with no Ruby, JVM, .NET, or Go runtime required on the target system.

The Go port should keep Braid's modern behavior recognizable while using a simpler, safer implementation where exact legacy compatibility would add complexity without current value.

## Scope Boundaries

In scope:

- A Go CLI named `braid`.
- Bazel-first build, test, and release workflow using Bzlmod.
- Runtime dependency on the `git` executable on `PATH`.
- Modern `.braids.json` config format only.
- Commands matching the current Ruby surface where practical: `add`, `update`, `remove`, `diff`, `push`, `setup`, `version`, and `status`.
- Extensive unit, integration, parity, and cross-platform test coverage.
- Clear inline comments for non-obvious Git/process choices.

Out of scope:

- Embedding or reimplementing Git.
- Supporting legacy `.braids` YAML/PStore config.
- Supporting removed SVN or full-history mirror formats.
- Supporting non-Git upstreams.
- Maintaining Ruby runtime compatibility.
- Adding feature flags for old behavior unless explicitly approved.

## Locked Decisions

- Language: Go.
- Build system: Bazel.
- Runtime Git strategy: call the `git` executable with argument arrays, never through a shell.
- Primary design goal: clarity, maintainability, and correctness over clever abstraction.
- Config baseline: modern `.braids.json` with `config_version: 1` and `mirrors`.
- Tests should exceed the Ruby suite's coverage and include negative/error/security cases.
- Implementation must be idiomatic Go and simple enough for a non-Go expert maintainer to read.

## Current Baseline Evidence

- Ruby command surface is defined in `vendor/braid/lib/braid/main.rb` and includes 9 modes.
- Ruby code declares `.braids.json`, `.braids`, and `REQUIRED_GIT_VERSION = '2.8.0'` in `vendor/braid/lib/braid.rb`.
- Ruby implementation delegates critical behavior to Git commands including `merge-recursive`, `read-tree`, `update-index`, `write-tree`, `diff`, `fetch`, and `push`.
- Ruby test suite currently has 133 checked-in RSpec examples in `.rb` files; a local run after `bundle install` completed with `143 examples, 0 failures`.
- The workspace currently has no root Bazel files, so the Go/Bazel layout starts from a clean root.

## Command Surface Expectations

The Go port should implement these commands with compatible names, required arguments, and high-level behavior:

- Global cache flags, placed before the command: `--no-cache` and `--cache-dir <path>`.
- `braid add <url> [local_path] [--branch|-b <branch>] [--tag|-t <tag>] [--revision|-r <rev>] [--path|-p <remote_path>] [--verbose|-v]`
- `braid update [local_path] [--branch|-b <branch>] [--tag|-t <tag>] [--revision|-r <rev>] [--keep] [--verbose|-v]`
- `braid remove <local_path> [--keep] [--verbose|-v]`
- `braid diff [local_path] [--keep] [--verbose|-v] [-- <git_diff_arg>...]`
- `braid push <local_path> [--branch|-b <branch>] [--keep] [--verbose|-v]`
- `braid setup [local_path] [--force|-f] [--verbose|-v]`
- `braid version`
- `braid status [local_path] [--verbose|-v]`

`upgrade-config` is intentionally not implemented in the Go port.

Help text and ordinary command messages will use a new idiomatic Go design. Compatibility is required for behavior, exit codes, config, and Git side effects, not Ruby message text.

## Behavior Expectations

- Run only at the root of a Git working tree for v1. Subdirectory execution is a future enhancement after the core port is stable.
- Refuse unsafe mirror paths up front: empty paths, absolute paths, paths with `..`, paths under `.git`, and path forms that cannot be represented safely across supported OSes.
- Preserve modern mirror semantics: branch, tag, revision lock, optional upstream subdirectory, optional single-file mirror, and stable remote naming.
- Preserve local cache behavior by default for performance. Default values are sourced from `BRAID_USE_LOCAL_CACHE` and `BRAID_LOCAL_CACHE_DIR`; CLI override flags are tracked by Q-10.
- Preserve Git conflict behavior for `update`: leave worktree/index in a state the user can resolve, and write the prepared merge message where Git expects it.
- Preserve `diff` pass-through after `--` without shell interpolation.
- Do not silently recover from unexpected Git states. Return clear diagnostics and leave evidence.

## Quality Gates

Required full gate before implementation tasks close:

```bash
bazel test //...
bazel build --platforms=@rules_go//go/toolchain:linux_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:linux_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_amd64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:darwin_arm64 //cmd/braid:braid
bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid
```

Additional release gate before an actual release:

- Run integration tests on native Linux, macOS, and Windows hosts because foreign-platform binaries cannot be executed by a normal local Bazel cross-build.
- Smoke-test each release binary with `braid version` and at least one fixture-backed `add` flow on its native OS.
- Produce raw binaries and checksums; document a manual signing/notarization path.
- CI provider and runner labels are not assumed by this plan. T16 must define the concrete native runner matrix before release automation is treated as complete.

Native release smoke matrix:

| OS | Architectures | Required smoke evidence before release cut |
|---|---|---|
| Linux | amd64, arm64 | Built artifact runs `braid version`; fixture-backed `braid add` succeeds in a temp repo. |
| macOS | amd64, arm64 | Built artifact runs `braid version`; fixture-backed `braid add` succeeds in a temp repo. |
| Windows | amd64 | Built artifact runs `braid version`; fixture-backed `braid add` succeeds in a temp repo. |

## Coverage Expectations

- Unit tests for config loading/writing, mirror model, CLI parsing, path validation, Git argument construction, and error mapping.
- Integration tests that create real Git repositories and exercise the same behavior users rely on.
- Characterization tests that compare selected Go behavior to Ruby Braid during the migration period only; final required gates are Go/Bazel-only.
- Cross-platform tests for path separators, spaces in paths, CRLF-sensitive files, executable bits where supported, and Windows-specific process behavior.
- Security tests for command argument handling and unsafe path rejection.
- Golden tests for `.braids.json` and expected diffs; output-message tests should focus on stable error categories rather than Ruby text.

## Known Intentional Divergences

- No YAML/PStore `.braids` support.
- No SVN mirror migration.
- No full-history mirror migration.
- No `upgrade-config` command.
- Output and help text may diverge from Ruby as long as behavior remains compatible.
- No `update --head` option; the Go CLI should reject it as an unknown flag.
- No v1 support for running commands from subdirectories of the downstream Git worktree.
- Unsafe path validation will be stricter than the Ruby implementation's TODO-covered behavior.
- Implementation will require Git 2.43.0 or newer.

## Preflight Matrix

| Command surface | Requires `git` on PATH | Requires worktree root | Requires config | Requires clean worktree | May write config/worktree |
|---|---|---|---|---|---|
| `version`, help, usage, parse errors | no | no | no | no | no |
| `setup` | yes | yes | yes | no | no |
| `status` | yes | yes | yes | no | no |
| `diff` | yes | yes | yes | no | no |
| `add` | yes | yes | no | yes | yes |
| `update` | yes | yes | yes | yes | yes |
| `remove` | yes | yes | yes | yes | yes |
| `push` | yes | yes | yes | no | temporary repo and remote only |

## Cache Contract

| Input | Behavior |
|---|---|
| No env vars and no flags | Cache enabled at default `~/.braid/cache`. |
| `BRAID_USE_LOCAL_CACHE` unset, `true`, or `1` | Cache enabled. |
| `BRAID_USE_LOCAL_CACHE` any other value | Cache disabled unless `--cache-dir` is supplied and `--no-cache` is not supplied. |
| `BRAID_LOCAL_CACHE_DIR` set | Use the expanded value as the default cache directory. |
| Global `--cache-dir <path>` | Cache enabled and uses the supplied path. |
| Global `--no-cache` | Cache disabled. |
| Both `--no-cache` and `--cache-dir` | Invalid usage. |
| Empty cache directory value | Invalid usage. |
| Relative cache directory value | Resolve relative to the current process working directory and store the absolute path in runtime state. |
| Tag and annotated-tag mirrors with cache disabled | Must still resolve tags using ordinary Git remotes; cache disabled must not make supported mirror modes fail. |

## Path Validation Contract

Local mirror paths and upstream `--path` values are validated separately. Local mirror paths affect the downstream worktree and must be stricter.

| Case | Local mirror path | Upstream `--path` | Rationale |
|---|---|---|---|
| Empty, `.`, absolute path, `..` segment | reject | reject | Prevent ambiguous writes and traversal. |
| Path under `.git` | reject | allow only as upstream content if Git can address it | Never write into downstream Git metadata. |
| Windows drive absolute or drive-relative path, UNC path | reject | reject | Keep config portable across target OSes. |
| Backslash path separators | reject | reject | `.braids.json` uses portable slash-separated paths. |
| Windows reserved basenames such as `CON`, `PRN`, `AUX`, `NUL`, `COM1`, `LPT1` | reject for local path | allow only if Git can address it and output local path is safe | Avoid checkout failures on Windows. |
| Trailing dot or space in any path element | reject for local path | reject | Avoid Windows/macOS ambiguity. |
| Colon in any path element | reject for local path | reject | Avoid Windows drive/path ambiguity and ref-name surprises. |
| Case-fold collision with an existing mirror path | reject | not applicable | Avoid ambiguous mirrors on case-insensitive filesystems. |

Remote names generated from mirror paths must be collision-checked before mutation. If two mirror paths normalize to the same remote name, the command must fail with a clear diagnostic.

## Commit And Push Metadata Contract

| Operation | Contract |
|---|---|
| `add` commit | Subject `Braid: Add mirror '<path>' at '<short-revision>'`; author/committer come from normal Git config; commit is created with `git commit --no-verify`. |
| `update` commit | Subject `Braid: Update mirror '<path>' to '<short-revision>'`; author/committer come from normal Git config; commit is created with `git commit --no-verify`. |
| `remove` commit | Subject `Braid: Remove mirror '<path>'`; author/committer come from normal Git config; commit is created with `git commit --no-verify`. |
| update conflict | Write the same update subject to Git's `MERGE_MSG` path and leave index/worktree for manual resolution. |
| `push` temp repo | Copy local `user.name`, `user.email`, and `commit.gpgsign` into the temp repo when present. |
| `push` interactive commit | Use `git commit -v`; if the editor/commit fails or creates no commit, do not push and clean up temp resources. |
| push failure | Do not delete or mutate downstream mirror config; clean up temporary repositories where practical and report the Git failure. |

## Update-All Contract

`braid update` without `local_path` is intentionally constrained:

- Updates every mirror that tracks a branch or tag.
- Skips revision-locked mirrors.
- Rejects `--branch`, `--tag`, or `--revision` when `local_path` is omitted.
- Stops before each mirror if the downstream worktree is dirty.
- Continues in deterministic config path order unless a mirror update fails; on failure, stop and report the failed mirror.

## Open Questions Register

### Q-01

- status: resolved
- question: Which release target set should the plan support first?
- context: The user asked for Linux, macOS, and Windows standalone executables. Bazel/rules_go can cross-build many targets, but every added artifact increases CI, signing/notarization, smoke testing, and support burden.
- options:
  - A: Initial mainstream set: `linux_amd64`, `linux_arm64`, `darwin_amd64`, `darwin_arm64`, `windows_amd64`.
  - B: Minimal set: `linux_amd64`, `darwin_arm64`, `windows_amd64`.
  - C: Broad set: option A plus `windows_arm64` and additional Linux architectures.
- tradeoffs:
  - A balances modern coverage and maintenance cost.
  - B is fastest to implement and validate but may disappoint Linux ARM and Intel Mac users.
  - C maximizes coverage but raises test/support cost before the port has proven itself.
- recommended_default: A. It covers the practical mainstream matrix without exploding support scope.
- user_decision: A. Initial release target set is `linux_amd64`, `linux_arm64`, `darwin_amd64`, `darwin_arm64`, and `windows_amd64`.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`, `40-test-strategy.md`

### Q-02

- status: resolved
- question: What minimum Git version should the Go port require?
- context: Ruby Braid declares Git 2.8.0, but `push` has conditional complexity around Git 2.27 sparse checkout behavior. A newer minimum removes old-path handling and aligns with the no-historic-infrastructure goal.
- options:
  - A: Keep Git 2.8.0 parity.
  - B: Require Git 2.27.0.
  - C: Require a newer Git available in current supported OS package managers.
- tradeoffs:
  - A maximizes compatibility but preserves old complexity.
  - B removes known `push` branching and is old enough for most modern developer machines.
  - C simplifies future Git assumptions further but may exclude stable enterprise machines.
- recommended_default: B.
- user_decision: C. Require a newer Git available through current supported OS package managers rather than preserving Ruby Braid's 2.8.0 floor or stopping at 2.27.0.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`

### Q-03

- status: resolved
- question: Should `upgrade-config` remain as a no-op/diagnostic command or be removed?
- context: The modern port will not support YAML/PStore `.braids` or removed mirror types, so the Ruby upgrade command loses most of its purpose. Keeping the command preserves CLI familiarity.
- options:
  - A: Keep `upgrade-config`; report "already current" for valid modern config and a clear error for unsupported legacy config.
  - B: Remove `upgrade-config` and make it an unknown command.
  - C: Keep full legacy upgrade behavior except YAML.
- tradeoffs:
  - A preserves command discoverability with little complexity.
  - B is cleanest but breaks existing scripts that harmlessly call it.
  - C adds legacy surface without matching the stated scope.
- recommended_default: A.
- user_decision: B. Remove `upgrade-config` entirely; it should be treated as an unknown command because historic config migration is out of scope.
- artifacts_updated: `00-requirements.md`, `01-command-parity.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`

### Q-04

- status: resolved
- question: How exact should output/help text compatibility be?
- context: Exact text parity can make tests brittle and preserve Ruby-specific wording. Loose parity is simpler but may surprise scripts that parse output.
- options:
  - A: Exact where scripts likely depend on it: exit codes, `.braids.json`, Git side effects, and key status/diff markers; idiomatic/helpful wording elsewhere.
  - B: Byte-for-byte output parity for all commands.
  - C: New output design with no parity guarantees except behavior.
- tradeoffs:
  - A balances user compatibility and maintainability.
  - B greatly increases effort and blocks idiomatic Go CLI design.
  - C is clean but too risky for a port.
- recommended_default: A.
- user_decision: C. Use a new output/help design with behavior parity only; do not preserve Ruby wording except where a message category is needed for clarity.
- artifacts_updated: `00-requirements.md`, `01-command-parity.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`, `40-test-strategy.md`

### Q-05

- status: resolved
- question: Should the Go port preserve Braid's local cache environment variables?
- context: Ruby Braid uses `BRAID_USE_LOCAL_CACHE` and `BRAID_LOCAL_CACHE_DIR`. Keeping them helps existing users and tests, but cache behavior adds filesystem and remote state complexity.
- options:
  - A: Preserve both environment variables and default cache behavior.
  - B: Preserve cache directory only, but require explicit opt-in.
  - C: Remove local cache support.
- tradeoffs:
  - A maximizes behavioral parity.
  - B is safer and more explicit but changes default performance/behavior.
  - C simplifies code but may slow repeated operations and break workflows.
- recommended_default: A unless tests show cache behavior is a disproportionate source of complexity.
- user_decision: Preserve local cache support and keep it enabled by default for performance. Default behavior should continue to honor existing `BRAID_USE_LOCAL_CACHE` and `BRAID_LOCAL_CACHE_DIR` environment variables. Add CLI override flags for disabling cache and selecting cache location, with exact flag names tracked by Q-10.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`, `40-test-strategy.md`

### Q-06

- status: resolved
- question: Should tests keep invoking Ruby Braid as an oracle after the initial port?
- context: Characterization against Ruby is valuable while porting, but a long-term Ruby test dependency conflicts with the runtime-independence goal.
- options:
  - A: Use Ruby oracle tests during migration only; final required gates are Go/Bazel-only.
  - B: Keep Ruby oracle tests permanently as optional compatibility tests.
  - C: Do not invoke Ruby at all; manually port expected behavior into Go tests.
- tradeoffs:
  - A gives confidence without permanent Ruby dependency.
  - B maximizes parity detection but keeps legacy tooling alive.
  - C is clean but increases risk of missing subtle behavior.
- recommended_default: A.
- user_decision: A. Use Ruby oracle tests during migration only; final required gates must be Go/Bazel-only.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`, `40-test-strategy.md`

### Q-07

- status: resolved
- question: Should release artifacts include signed/notarized packages or raw binaries only?
- context: Signing is useful for trust, especially on macOS and Windows, but requires account credentials and process overhead.
- options:
  - A: Raw binaries and checksums for the first port.
  - B: Raw binaries, checksums, and documented manual signing path.
  - C: Fully signed/notarized release pipeline from day one.
- tradeoffs:
  - A is simplest and enough for internal validation.
  - B keeps the plan ready for public release without blocking implementation.
  - C is polished but adds operational dependencies unrelated to port correctness.
- recommended_default: B.
- user_decision: B. First release artifacts should be raw binaries and checksums, with a documented manual signing/notarization path rather than a fully automated signing pipeline.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`, `40-test-strategy.md`

### Q-08

- status: resolved
- question: Should the implementation support running from subdirectories of the downstream Git worktree?
- context: Ruby Braid explicitly rejects running outside the repository root. Supporting subdirectories is user-friendly but touches path normalization throughout the tool.
- options:
  - A: Preserve root-only behavior for the port.
  - B: Add subdirectory support after core parity is stable.
  - C: Add subdirectory support in v1.
- tradeoffs:
  - A keeps the port focused and compatible with existing behavior.
  - B gives a clear future enhancement path.
  - C improves UX but expands risk in every command.
- recommended_default: A, with B as a future task.
- user_decision: B. Preserve root-only behavior for v1 and explicitly track subdirectory execution as a future enhancement after core parity is stable.
- artifacts_updated: `00-requirements.md`, `01-command-parity.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`, `40-test-strategy.md`

### Q-09

- status: resolved
- question: What exact Git version should implement the Q-02 "current package manager" policy?
- context: Current package evidence checked on 2026-06-14: Ubuntu 24.04 LTS packages Git 2.43.0; Ubuntu 26.04 LTS packages Git 2.53.0; Debian 13 packages Git 2.47.3; Homebrew and Git for Windows list Git 2.54.0. The selected floor determines which "current" systems work out of the box.
- options:
  - A: Git 2.43.0, anchored to Ubuntu 24.04 LTS.
  - B: Git 2.47.0, anchored near Debian 13 stable.
  - C: Git 2.53.0, anchored to Ubuntu 26.04 LTS.
- tradeoffs:
  - A supports the widest current mainstream Linux LTS base while still being far newer than Ruby Braid's 2.8.0.
  - B is a stricter modern floor but excludes Ubuntu 24.04 LTS default Git.
  - C is very current but excludes Ubuntu 24.04 LTS and Debian 13 default Git.
- recommended_default: A.
- user_decision: A. Require Git 2.43.0 or newer, anchored to Ubuntu 24.04 LTS while still being much newer than Ruby Braid's 2.8.0 floor.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`, `40-test-strategy.md`

### Q-10

- status: resolved
- question: What exact CLI flags should control local cache behavior?
- context: Q-05 resolved that cache is necessary, on by default, and should still source defaults from the existing environment variables. CLI flags are new behavior and must be consistent across subcommands without making parsing complex.
- options:
  - A: Global flags before the command: `braid --no-cache ...` and `braid --cache-dir <path> ...`.
  - B: Per-command flags after the command on cache-using commands: `braid add --no-cache ...`, `braid add --cache-dir <path> ...`.
  - C: Support both global and per-command forms.
- tradeoffs:
  - A is simplest and idiomatic for cross-cutting process configuration, but differs from Ruby's command-specific flag style.
  - B keeps flags near the command but duplicates parser behavior across commands.
  - C is most flexible but adds complexity and more tests.
- recommended_default: A.
- user_decision: A. Use global flags before the command: `braid --no-cache <command> ...` and `braid --cache-dir <path> <command> ...`.
- artifacts_updated: `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml`, `30-compatibility-matrix.md`, `40-test-strategy.md`
