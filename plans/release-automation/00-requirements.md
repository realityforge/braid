# Release Automation Requirements

Status: accepted
Last updated: 2026-06-15

## Mission

Automate Braid releases from GitHub Actions while keeping Bazel responsible for
build/test primitives, GitHub Actions responsible for native runners and release
permissions, and release scripts responsible for small orchestration logic.

The first automation must cut stable releases from `main`, stamp release
binaries from Git tags, build raw platform binaries plus checksums, and create a
draft GitHub release for human publication.

## Baseline Evidence

- `internal/cli/cli.go` currently declares `const DefaultVersion = "0.0.0-dev"`.
- `integration/braid_integration_test.go` currently expects `braid 0.0.0-dev`.
- `cmd/braid/BUILD.bazel` currently exposes one `go_binary` target:
  `//cmd/braid:braid`.
- `.github/workflows/ci.yml` currently has `Go quality and lint` plus four
  integration jobs: windows-amd64, linux-arm64, darwin-amd64, darwin-arm64.
- `docs/release.md` documents five release targets and raw binary artifacts:
  linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64.exe.
- Local rules_go implementation supports link-time stamping with `x_defs`;
  unresolved stamp keys are not passed to the Go linker, so unstamped builds keep
  source defaults.

## Scope

In scope:

- Bazel/rules_go stamping for release version output.
- GitHub Actions `workflow_dispatch` release-cut workflow.
- Tag-ref release build workflow dispatched by release-cut.
- Checked-in Bash scripts for release validation and orchestration.
- Focused Bazel `sh_test` coverage for deterministic shell logic.
- Native five-platform release build matrix.
- Draft GitHub release with raw binaries and `SHA256SUMS`.
- Minimal release documentation that points to workflow-owned behavior.

Out of scope for first implementation:

- Local release command as primary entrypoint.
- Source version patch/release/post-release bump commits.
- Prerelease support.
- Archive packaging.
- Automatic release publication.
- Automatic rollback/delete of tags or releases.
- Signed Git tags.
- Automated macOS signing or notarization.
- ShellCheck integration.
- Expanding normal PR/push CI to the full release matrix.

## Command Surface And Interfaces

- `.github/workflows/release-cut.yml`
  - Trigger: `workflow_dispatch`.
  - Input: `version`, accepting `0.1.0` or `v0.1.0`.
  - Permissions: `contents: write`, `checks: read`, and `actions: write`.
  - Token: use `GH_TOKEN: ${{ github.token }}` for `gh` commands.
  - Behavior: normalize to tag `vX.Y.Z`, abort unless the workflow ref is
    `main`, resolve `origin/main` HEAD, wait for required checks on that exact
    SHA, re-fetch `origin/main` immediately before tagging and abort if it
    changed, reject older/equal stable versions, create and push an annotated
    tag, then dispatch `release.yml` with `ref` set to the new tag.
  - Required checks:
    - `Go quality and lint`
    - `Integration (windows-amd64)`
    - `Integration (linux-arm64)`
    - `Integration (darwin-amd64)`
    - `Integration (darwin-arm64)`

- `.github/workflows/release.yml`
  - Trigger: `workflow_dispatch`.
  - Permissions: `contents: write` and `actions: read` for the publish job;
    matrix jobs should use narrower read-only permissions where practical.
  - Behavior: require the dispatch ref to be a stable tag, stamp version from
    that tag, run native release matrix, upload workflow artifacts, verify final
    artifact set, create draft GitHub release.

- `tools/release/*`
  - Bash scripts with `set -euo pipefail`.
  - Shared deterministic logic tested by Bazel `sh_test`.
  - GitHub side effects kept in scripts but validated primarily through
    workflow execution.

## Behavior Requirements

1. Development builds continue to print `braid 0.0.0-dev`.
2. Release builds for tag `vX.Y.Z` print `braid X.Y.Z`.
3. Release input must be stable semver only: `X.Y.Z` or `vX.Y.Z`, matching
   `^v?(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$`.
4. Existing stable tags matching `^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$`
   must be fetched and compared; requested version must be greater than the
   maximum. An empty existing-tag set is valid.
5. Release-cut must wait up to a bounded timeout for required checks on the
   exact `main` SHA. Every required check must complete with conclusion exactly
   `success`; all other completed conclusions, including `failure`,
   `cancelled`, `neutral`, `skipped`, `stale`, `timed_out`, and
   `action_required`, are immediate failures. Queued, in-progress, pending,
   requested, and waiting checks are wait states until timeout.
6. Release-cut must create an annotated tag with message `Braid X.Y.Z`.
7. Release workflow must use fixed native runners:
   - `ubuntu-24.04` for linux-amd64.
   - `ubuntu-24.04-arm` for linux-arm64.
   - `macos-15-intel` for darwin-amd64.
   - `macos-15` for darwin-arm64.
   - `windows-2025` for windows-amd64.
8. Release artifacts must be named:
   - `braid-linux-amd64`
   - `braid-linux-arm64`
   - `braid-darwin-amd64`
   - `braid-darwin-arm64`
   - `braid-windows-amd64.exe`
   - `SHA256SUMS`
9. Matrix jobs must upload workflow artifacts, not release assets directly.
10. Final publish job must verify the complete artifact set before creating the
    draft release.
11. Draft release title must be `Braid X.Y.Z`.
12. Draft release notes must use GitHub auto-generated notes.
13. Workflow YAML should contain inline comments for workflow-specific behavior;
    docs must not duplicate obvious workflow details.
14. Workflow concurrency must prevent overlapping release cuts and same-tag
    release builds.
15. Release-cut must not rely on the tag push caused by `GITHUB_TOKEN` to start
    release packaging; it must dispatch `release.yml` explicitly after the tag
    exists.
16. `release.yml` must reject any dispatch that does not run against a stable
    tag ref.
17. macOS artifacts produced by first automation are intentionally unsigned raw
    binaries. If a human signs/notarizes macOS assets before publication, they
    must replace those release assets and regenerate `SHA256SUMS` before
    publishing.

## Quality Gates

Targeted validation during implementation:

- Version normalization and semver comparison shell tests through Bazel
  `sh_test`, including leading-zero rejection, equal/older/newer comparisons,
  and empty existing-tag sets.
- Unstamped binary check: `bazel run //cmd/braid:braid -- version`.
- Stamped binary check with:
  `bazel run --stamp --embed_label=1.2.3 //cmd/braid:braid -- version`.
- Stamped integration check with:
  `bazel test --stamp --embed_label=1.2.3 --test_env=BRAID_EXPECTED_VERSION=1.2.3 //integration:braid_integration_test`.
- Native platform artifact smoke in release matrix: copied artifact runs
  `version` and prints the stamped version.
- Workflow YAML syntax sanity checks where local tooling can verify without
  contacting GitHub.

Required full local gate before implementation completion:

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
```

Required remote validation before the automation is considered release-ready:

- `release-cut.yml` can be manually dispatched against `main` with a real next
  version, creates the tag, and dispatches `release.yml` on that tag ref.
- `release.yml` completes the five-platform matrix for the dispatched tag ref.
- Draft release contains exactly the expected assets and checksums.

## Intentional Divergences

- The automated release path supersedes the manual build command block as the
  primary release path.
- Normal CI remains lighter than the release matrix; release-only linux-amd64
  remains explicit in `release.yml`.
- Manual macOS signing/notarization remains documented as a separate process
  until credentials and artifact handling are approved.

## External Prerequisites

- The repository default branch is `main`.
- `main` branch protection is recommended before first public release but is not
  configured by this plan. The workflow still gates on exact-SHA CI and tags
  only `origin/main`; repository administrators own branch protection setup.

## Open Questions Register

All questions below were resolved in the planning interview. No open questions
remain, but plan acceptance is still pending user review.

| ID | Status | Question | Context | Options | Tradeoffs | Recommended Default | User Decision | Artifacts Updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | Source version patches or tag-driven stamping? | `DefaultVersion` and integration expectation are hardcoded to dev. | Patch source; stamp from tag. | Source patch creates release/post-release churn; stamping removes version commits. | Stamp from tag. | Accepted. | Requirements, implementation plan, task board. |
| Q-02 | resolved | Tag and binary version format? | Git tags often use `v`, CLI output usually omits it. | Binary prints `vX.Y.Z`; binary prints `X.Y.Z`. | Keeping `v` in tags preserves Git convention; omitting it in CLI keeps semver output clean. | Tag `vX.Y.Z`, binary `X.Y.Z`. | Accepted. | Requirements, implementation plan. |
| Q-03 | resolved | Create GitHub release automatically? | Release assets should be attached to the tag. | Manual release; automatic draft; automatic public release. | Draft automates assets without publishing incomplete content. | Automatic draft release. | Accepted. | Requirements, implementation plan. |
| Q-04 | resolved | Primary entrypoint? | Release no longer needs local source commits. | Local command; `workflow_dispatch`. | GitHub entrypoint gives audit trail and stable permissions. | `workflow_dispatch`. | Accepted. | Requirements, implementation plan. |
| Q-05 | resolved | Release `main` HEAD or arbitrary SHA? | Releases should come from the repository default branch; branch protection is an external prerequisite, not configured here. | Main HEAD; explicit SHA. | Arbitrary SHA adds provenance and branch-protection cases. | Main HEAD only. | Accepted. | Requirements, implementation plan. |
| Q-06 | resolved | One workflow or split workflows? | Tag should be the release source of truth. | Single workflow; release-cut plus tag-ref workflow. | Split keeps tag creation separate from tag-ref packaging; `GITHUB_TOKEN` tag pushes cannot be the handoff trigger. | Split workflows with explicit dispatch. | Accepted. | Requirements, implementation plan. |
| Q-07 | resolved | Require green normal CI before tagging? | Tagging should promote an already validated commit. | Require CI; rely only on release workflow. | Normal CI gate prevents tagging broken main commits. | Require exact-SHA CI. | Accepted. | Requirements, implementation plan. |
| Q-08 | resolved | Check workflow conclusion or specific checks? | Workflow-level success can hide matrix drift. | Workflow conclusion; required check names. | Named checks fail closed if expected jobs disappear. | Check specific names. | Accepted. | Requirements, implementation plan. |
| Q-09 | resolved | Expand normal CI matrix? | Release docs require five native rows; CI currently has four integration rows plus Ubuntu quality. | Expand CI; keep release matrix stricter. | Expanding CI costs more; release matrix can be exact. | Full matrix only in release workflow. | Accepted. | Requirements, implementation plan. |
| Q-10 | resolved | Stamp existing version symbol or add package? | Current version behavior is centralized in `cli.DefaultVersion`. | Existing symbol; new package. | New package adds indirection; existing symbol needs `const` to `var`. | Stamp `braid/internal/cli.DefaultVersion`. | Accepted. | Requirements, implementation plan. |
| Q-11 | resolved | Keep dev output for normal builds? | Local builds should remain unchanged. | Always stamped; release-only stamp. | Release-only stamp preserves existing local behavior. | Dev builds print `0.0.0-dev`. | Accepted. | Requirements, implementation plan. |
| Q-12 | resolved | Missing/pending CI behavior? | Main CI may still be running when release cut starts. | Fail immediately; bounded wait. | Waiting handles normal race; timeout prevents hanging forever. | Bounded wait. | Accepted. | Requirements, implementation plan. |
| Q-13 | resolved | Lightweight or annotated tag? | Releases are human-significant events. | Lightweight; annotated. | Annotated tags carry message, tagger, timestamp. | Annotated tag. | Accepted. | Requirements, implementation plan. |
| Q-14 | resolved | Signed tags? | Signing keys/secrets are not yet designed. | Sign now; defer. | Signing adds credential scope. | Do not sign first automation. | Accepted. | Requirements, implementation plan. |
| Q-15 | resolved | Create draft before or after builds? | Failed builds should not leave draft clutter. | Before matrix; after matrix. | Publishing job can verify complete artifacts first. | Create draft after builds pass. | Accepted. | Requirements, implementation plan. |
| Q-16 | resolved | Cross-compile or native build? | Release docs require native executable evidence. | Cross-compile all; native matrix builds. | Native builds verify each platform artifact can run. | Build on native runners. | Accepted. | Requirements, implementation plan. |
| Q-17 | resolved | Raw binaries or archives? | Current release contract says raw binaries plus checksums. | Raw binaries; archives. | Archives improve UX but add packaging policy. | Raw binaries first. | Accepted. | Requirements, implementation plan. |
| Q-18 | resolved | Release notes source? | No existing changelog process. | Manual file; generated notes. | Generated notes keep first automation small. | GitHub generated notes with explicit title. | Accepted. | Requirements, implementation plan. |
| Q-19 | resolved | Accepted version input format? | Operators may type with or without `v`. | `X.Y.Z`; `vX.Y.Z`; both. | Supporting both is convenient and unambiguous for stable semver. | Accept both, normalize. | Accepted. | Requirements, implementation plan. |
| Q-20 | resolved | Prerelease support? | RCs affect validation and release flags. | Include now; defer. | Deferring keeps first path stable-only. | No prereleases first pass. | Accepted. | Requirements, implementation plan. |
| Q-21 | resolved | Enforce monotonic versions? | Existing tags should prevent accidental downgrade. | No comparison; compare stable tags. | Simple stable comparison avoids interpreting all tag shapes. | Require greater than max stable tag. | Accepted. | Requirements, implementation plan. |
| Q-22 | resolved | Inline YAML logic or scripts? | Release logic includes parsing, polling, checksums. | Inline YAML; checked-in scripts. | Scripts are easier to test and review. | Thin YAML, checked-in scripts. | Accepted. | Requirements, implementation plan. |
| Q-23 | resolved | Script language? | Orchestration calls git, gh, bazel, shasum. | Bash; Go; other. | Bash is direct for first implementation. | Bash. | Accepted. | Requirements, implementation plan. |
| Q-24 | resolved | Test release scripts? | Pure shell logic is deterministic. | Manual only; Bazel `sh_test`. | Shell tests cover parsing without mocking GitHub. | Focused `sh_test`. | Accepted. | Requirements, implementation plan. |
| Q-25 | resolved | Add ShellCheck? | No ShellCheck is installed and repo has no shell tooling. | Add now; defer. | Adding toolchain is separate work. | Defer ShellCheck. | Accepted. | Requirements, implementation plan. |
| Q-26 | resolved | Windows script style? | Windows runner has Bash but artifact smoke is native Windows. | Bash only; PowerShell for Windows artifact check. | PowerShell is clearer for `.exe` execution. | Shared Bash plus Windows PowerShell check. | Accepted. | Requirements, implementation plan. |
| Q-27 | resolved | Matrix uploads direct to release? | Direct uploads can leave partial assets. | Direct release upload; workflow artifacts then publish. | Final job can verify complete set. | Use workflow artifacts. | Accepted. | Requirements, implementation plan. |
| Q-28 | resolved | Automate macOS signing/notarization? | Current docs defer signing credentials and notarization. | Automate now; keep manual. | Automation requires secret/storage design. | Keep manual. | Accepted. | Requirements, implementation plan. |
| Q-29 | resolved | Mark release latest? | Stable releases only in first pass. | Explicit latest; default automatic. | GitHub default is sufficient for stable releases. | Use default latest behavior. | Accepted. | Requirements, implementation plan. |
| Q-30 | resolved | Automatic rollback on failure? | Tags/releases are audit artifacts. | Delete automatically; leave state. | Auto-delete obscures history. | No rollback first pass. | Accepted. | Requirements, implementation plan. |
| Q-31 | resolved | Where should docs live? | Workflow files can carry operational comments. | Full docs; minimal docs/README. | Avoid duplicating workflow behavior. | Inline YAML docs; minimal release docs, possibly merge to README. | Accepted with explicit user refinement. | Requirements, implementation plan. |
| Q-32 | resolved | Publish draft automatically? | Final public boundary should be human-reviewed. | Publish automatically; stop at draft. | Human review catches notes/assets/caveats. | Stop at draft. | Accepted. | Requirements, implementation plan. |
| Q-33 | resolved | Dry-run mode? | Dry-run creates a second orchestration path. | Add dry run; no dry run. | Guardrails are simpler than simulated tagging. | No dry run first pass. | Accepted. | Requirements, implementation plan. |
| Q-34 | resolved | Workflow concurrency? | Concurrent release cuts can race on tags and main SHA. | Allow; serialize. | Serialization prevents overlapping release state. | Add concurrency groups. | Accepted. | Requirements, implementation plan. |
| Q-35 | resolved | Derive checks from branch protection? | Branch protection is external mutable state. | Derive externally; hardcode checked-in list. | Checked-in list is reviewable and fails closed. | Hardcode required checks. | Accepted. | Requirements, implementation plan. |
| Q-36 | resolved | Implement now or plan first? | Decisions should be reviewed before multi-file implementation. | Implement now; write plan first. | Plan prevents drift and supports review. | Write plan first, implement separately. | Accepted. | Requirements, implementation plan, task board. |
