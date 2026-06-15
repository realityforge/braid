#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: build-artifact.sh <platform> <version>

platforms:
  linux-amd64
  linux-arm64
  darwin-amd64
  darwin-arm64
  windows-amd64
USAGE
}

if [[ "$#" -ne 2 ]]; then
  usage
  exit 2
fi

platform=$1
version=$2

case "${platform}" in
  linux-amd64)
    bazel_platform='@rules_go//go/toolchain:linux_amd64'
    bazel_output='bazel-bin/cmd/braid/braid_/braid'
    artifact='braid-linux-amd64'
    ;;
  linux-arm64)
    bazel_platform='@rules_go//go/toolchain:linux_arm64'
    bazel_output='bazel-bin/cmd/braid/braid_/braid'
    artifact='braid-linux-arm64'
    ;;
  darwin-amd64)
    bazel_platform='@rules_go//go/toolchain:darwin_amd64'
    bazel_output='bazel-bin/cmd/braid/braid_/braid'
    artifact='braid-darwin-amd64'
    ;;
  darwin-arm64)
    bazel_platform='@rules_go//go/toolchain:darwin_arm64'
    bazel_output='bazel-bin/cmd/braid/braid_/braid'
    artifact='braid-darwin-arm64'
    ;;
  windows-amd64)
    bazel_platform='@rules_go//go/toolchain:windows_amd64'
    bazel_output='bazel-bin/cmd/braid/braid_/braid.exe'
    artifact='braid-windows-amd64.exe'
    ;;
  *)
    printf 'unknown release platform: %s\n' "${platform}" >&2
    usage
    exit 2
    ;;
esac

rm -rf dist
mkdir -p dist

MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' BRAID_VERSION="${version}" bazel build \
  --stamp \
  --workspace_status_command=tools/release/workspace-status.sh \
  --platforms="${bazel_platform}" \
  //cmd/braid:braid

if [[ "${platform}" == windows-* ]]; then
  cp "${bazel_output}" "dist/${artifact}"
else
  install -m 0755 "${bazel_output}" "dist/${artifact}"
fi

printf 'dist/%s\n' "${artifact}"
