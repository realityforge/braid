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

## Artifact Policy

The first automated release path publishes raw binaries plus `SHA256SUMS`.
Version tags use `vX.Y.Z`; binaries print `X.Y.Z`.

macOS artifacts are intentionally unsigned raw binaries. If a release is signed
or notarized before publication, replace the affected draft assets and
regenerate `SHA256SUMS` for the exact files that will be published.

## Repository Prerequisites

Branch protection for `main` is recommended before public releases. The release
automation gates on exact-SHA CI checks, but repository administrators still own
the branch-protection settings.
