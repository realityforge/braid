# Release Builds

This document defines the first Go/Bazel release process for Braid. It covers
raw binary artifacts, checksum generation, manual macOS signing/notarization,
and native smoke tests. It does not define automated publishing.

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

## Native Smoke Matrix

Foreign-platform Bazel builds prove compilation only. Before a release cut,
run the smoke test on each native OS/architecture row.

| Artifact | Native runner | Required smoke |
| --- | --- | --- |
| `braid-linux-amd64` | GitHub hosted `ubuntu-24.04` | `braid version`; fixture-backed `braid add` |
| `braid-linux-arm64` | GitHub hosted `ubuntu-24.04-arm` | `braid version`; fixture-backed `braid add` |
| `braid-darwin-amd64` | GitHub hosted `macos-15-intel` | `braid version`; fixture-backed `braid add` |
| `braid-darwin-arm64` | GitHub hosted `macos-15` | `braid version`; fixture-backed `braid add` |
| `braid-windows-amd64.exe` | GitHub hosted `windows-2025` | `braid version`; fixture-backed `braid add` |

Use fixed runner labels in release automation. Do not use `*-latest` labels for
release gates because GitHub documents them as moving labels.

### Linux And macOS Smoke

```bash
set -eu

bin="$PWD/dist/braid-linux-amd64"
case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) bin="$PWD/dist/braid-linux-amd64" ;;
  Linux-aarch64|Linux-arm64) bin="$PWD/dist/braid-linux-arm64" ;;
  Darwin-x86_64) bin="$PWD/dist/braid-darwin-amd64" ;;
  Darwin-arm64) bin="$PWD/dist/braid-darwin-arm64" ;;
  *) echo "unsupported smoke host: $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

"$bin" version

tmp="$(mktemp -d)"
upstream="$tmp/upstream"
downstream="$tmp/downstream"
git init --initial-branch=main "$upstream"
git -C "$upstream" config user.name 'Braid Release Smoke'
git -C "$upstream" config user.email 'braid-release@example.invalid'
printf 'release smoke\n' > "$upstream/README.md"
git -C "$upstream" add README.md
git -C "$upstream" commit -m 'seed upstream'

git init --initial-branch=main "$downstream"
git -C "$downstream" config user.name 'Braid Release Smoke'
git -C "$downstream" config user.email 'braid-release@example.invalid'
git -C "$downstream" config commit.gpgsign false
(
  cd "$downstream"
  "$bin" add "$upstream" vendor/smoke
  test -f vendor/smoke/README.md
  "$bin" status vendor/smoke
)
```

### Windows Smoke

```powershell
$ErrorActionPreference = "Stop"

$bin = Resolve-Path ".\dist\braid-windows-amd64.exe"
& $bin version

$tmp = New-Item -ItemType Directory -Path ([System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [System.Guid]::NewGuid()))
$upstream = Join-Path $tmp.FullName "upstream"
$downstream = Join-Path $tmp.FullName "downstream"

git init --initial-branch=main $upstream
git -C $upstream config user.name "Braid Release Smoke"
git -C $upstream config user.email "braid-release@example.invalid"
Set-Content -NoNewline -Path (Join-Path $upstream "README.md") -Value "release smoke`n"
git -C $upstream add README.md
git -C $upstream commit -m "seed upstream"

git init --initial-branch=main $downstream
git -C $downstream config user.name "Braid Release Smoke"
git -C $downstream config user.email "braid-release@example.invalid"
git -C $downstream config commit.gpgsign false
Push-Location $downstream
try {
  & $bin add $upstream vendor/smoke
  if (-not (Test-Path "vendor\smoke\README.md")) {
    throw "missing mirrored README"
  }
  & $bin status vendor/smoke
} finally {
  Pop-Location
}
```
