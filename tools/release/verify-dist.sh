#!/usr/bin/env bash
set -euo pipefail

dist=${1:-dist}
expected=(
  braid-linux-amd64
  braid-linux-arm64
  braid-darwin-amd64
  braid-darwin-arm64
  braid-windows-amd64.exe
  SHA256SUMS
)

for name in "${expected[@]}"; do
  if [[ ! -f "${dist}/${name}" ]]; then
    printf 'missing release artifact: %s/%s\n' "${dist}" "${name}" >&2
    exit 1
  fi
done

while IFS= read -r path; do
  name=$(basename "${path}")
  found=false
  for expected_name in "${expected[@]}"; do
    if [[ "${name}" == "${expected_name}" ]]; then
      found=true
      break
    fi
  done
  if [[ "${found}" == false ]]; then
    printf 'unexpected release artifact: %s\n' "${path}" >&2
    exit 1
  fi
done < <(find "${dist}" -maxdepth 1 -type f | sort)

for name in braid-linux-amd64 braid-linux-arm64 braid-darwin-amd64 braid-darwin-arm64; do
  if [[ ! -x "${dist}/${name}" ]]; then
    printf 'artifact is not executable: %s/%s\n' "${dist}" "${name}" >&2
    exit 1
  fi
done

if [[ -e "${dist}/braid-windows-amd64" ]]; then
  printf 'windows artifact must keep .exe suffix\n' >&2
  exit 1
fi

(cd "${dist}" && shasum -a 256 -c SHA256SUMS)
