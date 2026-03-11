#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

binaries=(
  "npm/platform/darwin-x64/bin/crabwise"
  "npm/platform/darwin-arm64/bin/crabwise"
  "npm/platform/linux-x64/bin/crabwise"
  "npm/platform/linux-arm64/bin/crabwise"
)

failures=()

for rel in "${binaries[@]}"; do
  path="${repo_root}/${rel}"
  reasons=()

  if [ ! -e "$path" ]; then
    reasons+=("missing")
  else
    if [ ! -f "$path" ]; then
      reasons+=("not regular file")
    fi
    if [ ! -x "$path" ]; then
      reasons+=("not executable")
    fi
    if [ -f "$path" ] && [ ! -s "$path" ]; then
      reasons+=("zero size")
    fi
  fi

  if [ "${#reasons[@]}" -gt 0 ]; then
    failures+=("${rel}: $(IFS=', '; printf '%s' "${reasons[*]}")")
  fi
done

if [ "${#failures[@]}" -gt 0 ]; then
  echo "invalid staged binaries:" >&2
  for failure in "${failures[@]}"; do
    echo "- ${failure}" >&2
  done
  exit 1
fi

echo "verified staged binaries"
