# Release Builds

This document defines the first Go/Bazel release process for Braid. It covers
raw binary artifacts, checksum generation, manual macOS signing/notarization,
native executable integration gates, and packaged-artifact checks. It does not
define automated publishing.

References checked on 2026-06-14:

- GitHub hosted runner labels:
  https://docs.github.com/en/actions/how-tos/write-workflows/choose-where-workflows-run/choose-the-runner-for-a-job
- GitHub larger runner labels and macOS arm64 limitations:
  https://docs.github.com/en/actions/reference/runners/larger-runners
- Apple notarization workflow:
  https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution
- Apple distribution signing guidance:
  https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac

## Release Targets

| Target | Bazel platform | Artifact |
| --- | --- | --- |
| Linux amd64 | `@rules_go//go/toolchain:linux_amd64` | `braid-linux-amd64` |
| Linux arm64 | `@rules_go//go/toolchain:linux_arm64` | `braid-linux-arm64` |
| macOS amd64 | `@rules_go//go/toolchain:darwin_amd64` | `braid-darwin-amd64` |
| macOS arm64 | `@rules_go//go/toolchain:darwin_arm64` | `braid-darwin-arm64` |
| Windows amd64 | `@rules_go//go/toolchain:windows_amd64` | `braid-windows-amd64.exe` |

## Build And Checksums

Run from the repository root. Copy each artifact immediately after its build,
because the `bazel-bin` symlink follows the most recent configuration.

```bash
rm -rf dist
mkdir -p dist

bazel build --platforms=@rules_go//go/toolchain:linux_amd64 //cmd/braid:braid
cp bazel-bin/cmd/braid/braid_/braid dist/braid-linux-amd64

bazel build --platforms=@rules_go//go/toolchain:linux_arm64 //cmd/braid:braid
cp bazel-bin/cmd/braid/braid_/braid dist/braid-linux-arm64

bazel build --platforms=@rules_go//go/toolchain:darwin_amd64 //cmd/braid:braid
cp bazel-bin/cmd/braid/braid_/braid dist/braid-darwin-amd64

bazel build --platforms=@rules_go//go/toolchain:darwin_arm64 //cmd/braid:braid
cp bazel-bin/cmd/braid/braid_/braid dist/braid-darwin-arm64

bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //cmd/braid:braid
cp bazel-bin/cmd/braid/braid_/braid.exe dist/braid-windows-amd64.exe

shasum -a 256 dist/braid-* > dist/SHA256SUMS
```

Do not generate checksums until after any signing step that changes artifact
bytes. The first release ships raw binaries plus `SHA256SUMS`.

## First Go Port Release Notes

- Artifacts are raw binaries named by OS and architecture.
- `SHA256SUMS` is generated for the exact files uploaded to the release.
- macOS signing and notarization are manual for the first release.
- Release automation is not complete until native
  `bazel test //integration:braid_integration_test` evidence is collected on
  the runner matrix below.

## Manual macOS Signing And Notarization

Signing is manual for the first release. Automated release jobs are incomplete
until signing identity storage, Apple credentials, and notarization artifact
handling are explicitly approved.

Set these variables on a macOS host with Xcode command-line tools installed:

```bash
export CODESIGN_IDENTITY='Developer ID Application: Example, Inc. (TEAMID)'
export NOTARY_PROFILE='braid-notary-profile'
```

Sign the macOS binaries with a secure timestamp and hardened runtime:

```bash
codesign --force --timestamp --options runtime --sign "$CODESIGN_IDENTITY" dist/braid-darwin-amd64
codesign --force --timestamp --options runtime --sign "$CODESIGN_IDENTITY" dist/braid-darwin-arm64
codesign --verify --strict --verbose=2 dist/braid-darwin-amd64
codesign --verify --strict --verbose=2 dist/braid-darwin-arm64
```

Submit zip archives to Apple notarization:

```bash
ditto -c -k --keepParent dist/braid-darwin-amd64 dist/braid-darwin-amd64.zip
ditto -c -k --keepParent dist/braid-darwin-arm64 dist/braid-darwin-arm64.zip
xcrun notarytool submit dist/braid-darwin-amd64.zip --keychain-profile "$NOTARY_PROFILE" --wait
xcrun notarytool submit dist/braid-darwin-arm64.zip --keychain-profile "$NOTARY_PROFILE" --wait
```

The first release still publishes raw binaries. If notarized zip archives are
also published, generate checksums for the exact files that will be uploaded.

## Native Integration Matrix

Foreign-platform Bazel builds prove compilation only. Before a release cut, run
the executable integration test on each native OS/architecture row. This target
builds and runs the Bazel `//cmd/braid:braid` executable as a subprocess against
generated local Git repositories, so packaged-artifact scripts should not
duplicate that behavior coverage.

| Artifact | Native runner | Required gate |
| --- | --- | --- |
| `braid-linux-amd64` | GitHub hosted `ubuntu-24.04` | `bazel test //integration:braid_integration_test` |
| `braid-linux-arm64` | GitHub hosted `ubuntu-24.04-arm` | `bazel test //integration:braid_integration_test` |
| `braid-darwin-amd64` | GitHub hosted `macos-15-intel` | `bazel test //integration:braid_integration_test` |
| `braid-darwin-arm64` | GitHub hosted `macos-15` | `bazel test //integration:braid_integration_test` |
| `braid-windows-amd64.exe` | GitHub hosted `windows-2025` | `bazel test //integration:braid_integration_test` |

Use fixed runner labels in release automation. Do not use `*-latest` labels for
release gates because GitHub documents them as moving labels.

## Packaged Artifact Checks

After the native integration matrix passes, keep packaged-artifact checks
focused on the files that will be uploaded:

- The copied binary launches and `version` exits successfully.
- `SHA256SUMS` matches the exact uploaded files.
- Unix artifacts have executable bits set.
- Windows artifacts keep the `.exe` suffix.
- macOS artifacts pass signing and notarization verification when signing is
  performed.

### Linux And macOS Artifacts

```bash
set -eu

test -x dist/braid-linux-amd64
test -x dist/braid-linux-arm64
test -x dist/braid-darwin-amd64
test -x dist/braid-darwin-arm64

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) dist/braid-linux-amd64 version ;;
  Linux-aarch64|Linux-arm64) dist/braid-linux-arm64 version ;;
  Darwin-x86_64) dist/braid-darwin-amd64 version ;;
  Darwin-arm64) dist/braid-darwin-arm64 version ;;
esac

shasum -a 256 -c dist/SHA256SUMS
```

### Windows Artifact

```powershell
$ErrorActionPreference = "Stop"

$bin = Resolve-Path ".\dist\braid-windows-amd64.exe"
& $bin version

$expected = Get-Content .\dist\SHA256SUMS | Where-Object { $_ -match "braid-windows-amd64\.exe$" }
if (-not $expected) {
  throw "missing checksum entry for braid-windows-amd64.exe"
}
$actual = (Get-FileHash -Algorithm SHA256 $bin).Hash.ToLowerInvariant()
if (-not $expected.StartsWith($actual)) {
  throw "checksum mismatch for braid-windows-amd64.exe"
}
```
