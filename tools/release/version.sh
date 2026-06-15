#!/usr/bin/env bash
set -euo pipefail

stable_input_re='^v?(0|[1-9][0-9]*)\.([0-9]|[1-9][0-9]*)\.([0-9]|[1-9][0-9]*)$'
stable_tag_re='^v(0|[1-9][0-9]*)\.([0-9]|[1-9][0-9]*)\.([0-9]|[1-9][0-9]*)$'

usage() {
  cat >&2 <<'USAGE'
usage: version.sh <command> [args]

commands:
  normalize <version>          print X.Y.Z for X.Y.Z or vX.Y.Z
  tag <version>                print vX.Y.Z for X.Y.Z or vX.Y.Z
  is-stable-tag <tag>          succeed only for vX.Y.Z stable tags
  assert-newer <version> [tag...]  require version greater than stable tags
USAGE
}

normalize_version() {
  local input=$1
  if [[ ! "${input}" =~ ${stable_input_re} ]]; then
    printf 'invalid stable version: %s\n' "${input}" >&2
    return 2
  fi
  printf '%s.%s.%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}"
}

stable_tag_version() {
  local input=$1
  if [[ ! "${input}" =~ ${stable_tag_re} ]]; then
    return 1
  fi
  printf '%s.%s.%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}"
}

version_greater_than() {
  local left=$1
  local right=$2
  local left_major left_minor left_patch right_major right_minor right_patch

  IFS=. read -r left_major left_minor left_patch <<<"${left}"
  IFS=. read -r right_major right_minor right_patch <<<"${right}"

  if ((10#${left_major} != 10#${right_major})); then
    ((10#${left_major} > 10#${right_major}))
    return
  fi
  if ((10#${left_minor} != 10#${right_minor})); then
    ((10#${left_minor} > 10#${right_minor}))
    return
  fi
  ((10#${left_patch} > 10#${right_patch}))
}

max_stable_version() {
  local max=''
  local tag version

  for tag in "$@"; do
    if ! version=$(stable_tag_version "${tag}"); then
      continue
    fi
    if [[ -z "${max}" ]] || version_greater_than "${version}" "${max}"; then
      max=${version}
    fi
  done

  printf '%s\n' "${max}"
}

require_arg_count() {
  local command=$1
  local want=$2
  local got=$3
  if [[ "${got}" -ne "${want}" ]]; then
    printf '%s requires %s argument(s), got %s\n' "${command}" "${want}" "${got}" >&2
    usage
    return 2
  fi
}

main() {
  local command=${1:-}
  if [[ -z "${command}" ]]; then
    usage
    return 2
  fi
  shift

  case "${command}" in
    normalize)
      require_arg_count "${command}" 1 "$#"
      normalize_version "$1"
      ;;
    tag)
      require_arg_count "${command}" 1 "$#"
      printf 'v%s\n' "$(normalize_version "$1")"
      ;;
    is-stable-tag)
      require_arg_count "${command}" 1 "$#"
      stable_tag_version "$1" >/dev/null
      ;;
    assert-newer)
      if [[ "$#" -lt 1 ]]; then
        printf 'assert-newer requires a requested version\n' >&2
        usage
        return 2
      fi
      local requested
      requested=$(normalize_version "$1")
      shift

      local max
      max=$(max_stable_version "$@")
      if [[ -n "${max}" ]] && ! version_greater_than "${requested}" "${max}"; then
        printf 'version %s is not greater than latest stable tag v%s\n' "${requested}" "${max}" >&2
        return 1
      fi
      printf 'v%s\n' "${requested}"
      ;;
    *)
      printf 'unknown version command: %s\n' "${command}" >&2
      usage
      return 2
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
