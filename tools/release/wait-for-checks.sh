#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: wait-for-checks.sh <sha> <timeout-seconds> <interval-seconds> <check-name>...

Poll GitHub check runs for the exact commit SHA until every named check has
completed with conclusion success. Any other completed conclusion fails
immediately.
USAGE
}

if [[ "$#" -lt 4 ]]; then
  usage
  exit 2
fi

sha=$1
timeout_seconds=$2
interval_seconds=$3
shift 3
required_checks=("$@")

repo=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
deadline=$(( $(date +%s) + timeout_seconds ))

while true; do
  check_rows=$(gh api \
    -H 'Accept: application/vnd.github+json' \
    "repos/${repo}/commits/${sha}/check-runs?per_page=100" \
    --jq '.check_runs | sort_by(.started_at // .created_at // "") | .[] | [.name, .status, (.conclusion // "")] | @tsv')

  waiting=()
  all_success=true

  for required in "${required_checks[@]}"; do
    match_status=''
    match_conclusion=''

    while IFS=$'\t' read -r name status conclusion; do
      if [[ "${name}" == "${required}" ]]; then
        match_status=${status}
        match_conclusion=${conclusion}
      fi
    done <<<"${check_rows}"

    case "${match_status}:${match_conclusion}" in
      completed:success)
        ;;
      completed:*)
        printf 'required check %q completed with non-success conclusion %q\n' "${required}" "${match_conclusion}" >&2
        exit 1
        ;;
      :)
        all_success=false
        waiting+=("${required}: missing")
        ;;
      queued:*|in_progress:*|pending:*|requested:*|waiting:*)
        all_success=false
        waiting+=("${required}: ${match_status}")
        ;;
      *)
        all_success=false
        waiting+=("${required}: ${match_status:-unknown}")
        ;;
    esac
  done

  if [[ "${all_success}" == true ]]; then
    printf 'all required checks succeeded for %s\n' "${sha}"
    exit 0
  fi

  now=$(date +%s)
  if (( now >= deadline )); then
    printf 'timed out waiting for required checks on %s:\n' "${sha}" >&2
    printf '  %s\n' "${waiting[@]}" >&2
    exit 1
  fi

  printf 'waiting for required checks on %s:\n' "${sha}"
  printf '  %s\n' "${waiting[@]}"
  sleep "${interval_seconds}"
done
