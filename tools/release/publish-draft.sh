#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 3 ]]; then
  printf 'usage: publish-draft.sh <tag> <version> <dist-dir>\n' >&2
  exit 2
fi

tag=$1
version=$2
dist=$3

gh release create "${tag}" \
  "${dist}/braid-linux-amd64" \
  "${dist}/braid-linux-arm64" \
  "${dist}/braid-darwin-amd64" \
  "${dist}/braid-darwin-arm64" \
  "${dist}/braid-windows-amd64.exe" \
  "${dist}/SHA256SUMS" \
  --draft \
  --verify-tag \
  --title "Braid ${version}" \
  --generate-notes
