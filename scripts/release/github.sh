#!/usr/bin/env bash
set -euo pipefail

command=${1:?Expected state, ensure-draft, upload, or publish}
version=${2:?Expected release tag}
shift 2
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

transient_error() {
  grep -Eq 'HTTP (429|5[0-9][0-9])([: ]|$)' "$scratch/error"
}

retry_delay() {
  echo "Retry GitHub release request ($1/5)." >&2
  sleep "$((1 << $1))"
}

release_state() {
  local attempt
  for attempt in 1 2 3 4 5; do
    if gh release view "$version" --json isDraft > "$scratch/state" 2> "$scratch/error"; then
      jq -er 'if .isDraft == true then "draft" elif .isDraft == false then "published" else error("Invalid release response") end' "$scratch/state"
      return
    fi
    if [[ "$(cat "$scratch/error")" == 'release not found' ]]; then
      echo missing
      return
    fi
    cat "$scratch/error" >&2
    if ! transient_error || [[ "$attempt" == 5 ]]; then
      return 1
    fi
    retry_delay "$attempt"
  done
}

mutate_release() {
  local operation=$1 attempt state
  shift
  for attempt in 1 2 3 4 5 6; do
    state=$(release_state) || return 1
    case "$operation:$state" in
      ensure-draft:draft|publish:published) return 0 ;;
      ensure-draft:missing|upload:draft|publish:draft) ;;
      *) echo "Cannot $operation release $version: $state." >&2; return 1 ;;
    esac
    # Reconcile the final failed request before reporting exhaustion.
    if [[ "$attempt" == 6 ]]; then
      echo 'GitHub release retries exhausted.' >&2
      return 1
    fi
    if gh release "$@" > "$scratch/result" 2> "$scratch/error"; then
      cat "$scratch/result" >&2
      return 0
    fi
    cat "$scratch/error" >&2
    if ! transient_error; then
      return 1
    fi
    if [[ "$attempt" != 5 ]]; then
      retry_delay "$attempt"
    fi
  done
}

case "$command" in
  state) release_state ;;
  ensure-draft)
    mutate_release ensure-draft create "$version" --verify-tag --draft \
      --title "Toolbox $version" --generate-notes
    ;;
  upload)
    [[ "$#" -gt 0 ]] || { echo 'Expected release assets.' >&2; exit 1; }
    mutate_release upload upload "$version" "$@" --clobber
    ;;
  publish) mutate_release publish edit "$version" --draft=false --latest ;;
  *) echo "Unknown release operation: $command" >&2; exit 1 ;;
esac
