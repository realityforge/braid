# Release Automation Implementation Plan

Status: accepted
Last updated: 2026-06-15

## Phase Sequence

1. Finalize this accepted plan after user review.
2. Add release version stamping without changing default development output.
3. Add release shell scripts and focused Bazel tests for deterministic logic.
4. Add the release-cut workflow that validates `main`, creates annotated tags,
   and dispatches the release workflow on the tag ref.
5. Add the tag-ref release workflow that builds artifacts and creates a draft
   GitHub release.
6. Minimize release documentation and move operational detail into workflow
   comments.
7. Run local gates and record remote validation expectations.

## Delivery Approach

- Execute one task at a time with scoped diffs.
- Prefer existing Bazel and GitHub Actions patterns from this repo.
- Keep workflow YAML responsible for triggers, permissions, matrix, and inline
  workflow comments.
- Keep parsing, polling, and artifact verification in checked-in Bash scripts.
- Do not implement release code until this draft plan is reviewed.

## Implementation Details

### Phase 1: Version Stamping

- Change `internal/cli.DefaultVersion` from `const` to `var` so rules_go can
  stamp it at link time.
- Add `x_defs` to `//cmd/braid:braid` for
  `braid/internal/cli.DefaultVersion = "{STABLE_BRAID_VERSION}"`.
- Add `tools/release/workspace-status.sh` that emits
  `STABLE_BRAID_VERSION <version>` when release workflows set `BRAID_VERSION`.
- Verify unstamped `bazel run //cmd/braid:braid -- version` still prints
  `braid 0.0.0-dev`.
- Verify stamped builds print `braid X.Y.Z` with:
  `BRAID_VERSION=1.2.3 bazel run --stamp --workspace_status_command=tools/release/workspace-status.sh //cmd/braid:braid -- version`.
- Update integration test version assertion so normal tests expect
  `0.0.0-dev`, while release matrix tests can set `BRAID_EXPECTED_VERSION`.
  Release matrix tests must pass it explicitly with:
  `--test_env=BRAID_EXPECTED_VERSION=$VERSION`.

### Phase 2: Release Scripts

Create small strict Bash scripts under `tools/release/`:

- `version.sh`: normalize `X.Y.Z` / `vX.Y.Z`, reject prerelease/build metadata,
  reject leading-zero numeric components, compare stable semver values, and
  accept an empty existing-tag set.
- `wait-for-checks.sh`: poll required check names for an exact SHA with bounded
  timeout. Required checks pass only when their conclusion is exactly
  `success`; every other completed conclusion fails immediately, while queued,
  in-progress, pending, requested, and waiting statuses continue polling until
  timeout.
- `build-artifact.sh`: build one platform artifact with Bazel stamping and copy
  it to `dist/` with the approved name.
- `verify-dist.sh`: verify all expected artifact names, executable bits where
  relevant, Windows suffix, and checksums.
- `publish-draft.sh`: create the draft GitHub release with generated notes and
  verified tag.

Add `tools/release/BUILD.bazel` with `sh_test` coverage for `version.sh`.
GitHub side-effect scripts should be written to be readable and fail closed, but
not over-mocked locally.

### Phase 3: Release Cut Workflow

Add `.github/workflows/release-cut.yml`:

- Trigger: `workflow_dispatch`.
- Input: `version`.
- Permissions: `contents: write`, `checks: read`, and `actions: write`.
- Token: set `GH_TOKEN: ${{ github.token }}` for `gh` commands.
- Concurrency: `release-cut`, `cancel-in-progress: false`.
- Abort unless the workflow is running from `main`.
- Resolve `origin/main` HEAD.
- Normalize and validate version.
- Fetch tags and reject non-increasing stable versions.
- Wait for required check names on the exact SHA:
  - `Go quality and lint`
  - `Integration (windows-amd64)`
  - `Integration (linux-arm64)`
  - `Integration (darwin-amd64)`
  - `Integration (darwin-arm64)`
- Re-fetch `origin/main` immediately before tagging and abort if it differs
  from the checked SHA.
- Create and push annotated tag `vX.Y.Z` with message `Braid X.Y.Z`.
- Dispatch `.github/workflows/release.yml` with `ref` set to `vX.Y.Z`; do not
  rely on the `GITHUB_TOKEN` tag push to trigger a workflow run.

### Phase 4: Tag-Ref Release Workflow

Add `.github/workflows/release.yml`:

- Trigger: `workflow_dispatch`.
- Permissions: `contents: write` and `actions: read` for the publish job;
  matrix jobs should use read-only contents permissions where practical.
- Concurrency: `release-${{ github.ref_name }}`, `cancel-in-progress: false`.
- Reject the run unless the dispatch ref is a stable tag, then derive binary
  version from `github.ref_name`.
- Matrix builds:
  - linux-amd64 on `ubuntu-24.04`.
  - linux-arm64 on `ubuntu-24.04-arm`.
  - darwin-amd64 on `macos-15-intel`.
  - darwin-arm64 on `macos-15`.
  - windows-amd64 on `windows-2025`.
- Each matrix job:
  - Runs `bazel test //integration:braid_integration_test` with
    `--stamp`, `--workspace_status_command=tools/release/workspace-status.sh`,
    `BRAID_VERSION=$VERSION`, and
    `--test_env=BRAID_EXPECTED_VERSION=$VERSION`.
  - Builds the platform artifact with stamping.
  - Runs the copied artifact's `version` command natively.
  - Uploads the artifact as a workflow artifact.
- Final publish job:
  - Downloads workflow artifacts.
  - Verifies exact expected file set.
  - Generates `SHA256SUMS`.
  - Verifies checksums.
  - Creates draft release with title `Braid X.Y.Z`, generated notes, and all
    assets.
  - Treats macOS binaries as intentionally unsigned raw assets. If a human
    signs/notarizes before publication, they must replace the macOS assets and
    regenerate/upload `SHA256SUMS`.

### Phase 5: Documentation

- Keep operational detail as comments in workflow YAML where it is closest to
  the behavior.
- Reduce `docs/release.md` to the non-obvious release contract and manual
  publishing/signing caveats.
- If `docs/release.md` becomes very small, merge it into `README.md` and remove
  the separate release doc.
- Avoid duplicating matrix definitions or command sequences in prose.

## High-Risk Areas

- Risk: stamping accidentally changes normal development output.
  - Impact: local tests and existing documentation become misleading.
  - Mitigation: targeted unstamped and stamped version checks; keep release
    stamp opt-in through `--stamp --workspace_status_command`.
- Risk: release-cut blesses the wrong SHA.
  - Impact: tag provenance is wrong.
  - Mitigation: require the workflow ref to be `main`, resolve `origin/main`,
    poll checks for that exact SHA, re-fetch `origin/main` immediately before
    tagging, and abort if it changed.
- Risk: release workflow is not started after tag creation.
  - Impact: release-cut creates a tag but no artifacts or draft release.
  - Mitigation: explicitly dispatch `release.yml` with the tag ref after
    creating the tag; this avoids relying on `GITHUB_TOKEN` tag-push behavior.
- Risk: expected CI check names drift.
  - Impact: release cut could pass without an expected job, pass a skipped row,
    or fail unexpectedly.
  - Mitigation: hardcode checked-in names, require exact `success`
    conclusions, and fail closed when a required check is missing or completes
    with any other conclusion.
- Risk: partial release assets.
  - Impact: draft release has incomplete or mismatched files.
  - Mitigation: use workflow artifacts and one final publish job that verifies
    all expected assets before creating the draft.
- Risk: platform-specific script behavior.
  - Impact: Windows artifact checks fail due to shell/path assumptions.
  - Mitigation: keep shared logic in Bash on Unix-like jobs and use PowerShell
    for Windows-native artifact smoke.

## Required Full Gates

Run before marking implementation complete:

```bash
bazel run @rules_go//go -- fmt ./...
git diff --exit-code
bazel test //...
bazel run @rules_go//go -- vet ./...
bazel run @rules_go//go -- run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0 run
```

Remote release readiness evidence:

- `release-cut.yml` creates the intended annotated tag from `main` and
  dispatches `release.yml` on the tag ref.
- `release.yml` completes the five-platform matrix for that tag ref.
- The draft release contains only the approved artifact set and valid
  `SHA256SUMS`.

## Completion Criteria

- All planned tasks completed.
- No task has `commit.hash: pending`.
- `release-cut.yml` and `release.yml` exist and are documented inline.
- Workflow permissions are explicit and no broader than the plan requires.
- Normal development version remains `0.0.0-dev`.
- Release tag `vX.Y.Z` produces binaries that print `X.Y.Z`.
- Draft release workflow leaves the public publish step to a human.
- Minimal release docs do not duplicate workflow-owned details.
- Working tree is clean after committed implementation, unless explicitly
  deferred by the user.

## Decision Log

- Q-01: Use tag-driven Bazel stamping; remove source version patch/bump from
  release flow.
- Q-02: Tags use `vX.Y.Z`; binaries print `X.Y.Z`.
- Q-03: Automation creates a draft GitHub release.
- Q-04: Primary release entrypoint is GitHub `workflow_dispatch`.
- Q-05: Release only `origin/main` HEAD; branch protection remains an external
  repository prerequisite.
- Q-06: Split release cut and tag-ref release workflows; hand off with explicit
  workflow dispatch after tag creation.
- Q-07: Require green normal CI before tagging.
- Q-08: Gate on specific required check names.
- Q-09: Keep full five-platform native matrix release-only.
- Q-10: Stamp existing `braid/internal/cli.DefaultVersion`.
- Q-11: Preserve `0.0.0-dev` for normal builds.
- Q-12: Wait for checks with bounded timeout.
- Q-13: Create annotated tags.
- Q-14: Do not sign tags in first automation.
- Q-15: Create draft release only after builds pass.
- Q-16: Build artifacts on native runners.
- Q-17: Publish raw binaries plus `SHA256SUMS`.
- Q-18: Use GitHub generated release notes with explicit title.
- Q-19: Accept `X.Y.Z` or `vX.Y.Z` input.
- Q-20: Defer prerelease support.
- Q-21: Require requested stable version greater than existing stable tags.
- Q-22: Put nontrivial logic in checked-in scripts.
- Q-23: Use Bash for release scripts.
- Q-24: Add focused Bazel `sh_test` coverage for pure shell logic.
- Q-25: Defer ShellCheck integration.
- Q-26: Use PowerShell for Windows-native artifact smoke.
- Q-27: Use workflow artifacts before final publishing.
- Q-28: Keep macOS signing/notarization manual.
- Q-29: Let GitHub default latest behavior apply.
- Q-30: Do not automatically roll back tags/releases.
- Q-31: Prefer inline workflow documentation and minimal release docs.
- Q-32: Stop at draft release; human publishes.
- Q-33: Do not add dry-run mode first pass.
- Q-34: Add workflow concurrency controls.
- Q-35: Hardcode release-cut required checks in the repo.
- Q-36: Write and review this plan before implementation.
