#!/usr/bin/env bash
set -euo pipefail

if [[ -n "${BRAID_VERSION:-}" ]]; then
  printf 'STABLE_BRAID_VERSION %s\n' "${BRAID_VERSION}"
fi
