#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 vX.Y.Z" >&2
  exit 1
fi

tag="$1"
if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$ ]]; then
  echo "invalid tag: $tag (expected vX.Y.Z)" >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="${tag#v}"
base_url="https://github.com/crabwise-ai/crabwise/releases/download/${tag}"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

sha256_file="${tmp_dir}/checksums.txt"
checksum_names=(
  "checksums.txt"
  "sha256sums.txt"
  "SHA256SUMS"
  "crabwise_${version}_checksums.txt"
)

downloaded_checksums=0
for name in "${checksum_names[@]}"; do
  if curl -fsSL "${base_url}/${name}" -o "$sha256_file"; then
    downloaded_checksums=1
    break
  fi
done

if [ "$downloaded_checksums" -ne 1 ]; then
  echo "missing release checksums for ${tag}" >&2
  exit 1
fi

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  echo "missing sha256sum or shasum" >&2
  exit 1
}

expected_sum() {
  local file="$1"
  awk -v f="$file" '$2==f || $2=="*"f {print $1; exit}' "$sha256_file"
}

stage_one() {
  local os="$1"
  local artifact_arch="$2"
  local pkg_dir="$3"
  local tar_name="crabwise_${version}_${os}_${artifact_arch}.tar.gz"
  local tar_path="${tmp_dir}/${tar_name}"
  local expected
  local actual
  local entry
  local extract_dir="${tmp_dir}/extract-${os}-${artifact_arch}"
  local dest="${repo_root}/${pkg_dir}/bin/crabwise"

  curl -fsSL "${base_url}/${tar_name}" -o "$tar_path"

  expected="$(expected_sum "$tar_name")"
  if [ -z "$expected" ]; then
    echo "missing checksum entry for ${tar_name}" >&2
    exit 1
  fi

  actual="$(sha256 "$tar_path")"
  if [ "$expected" != "$actual" ]; then
    echo "checksum mismatch for ${tar_name}" >&2
    exit 1
  fi

  entry="$(tar -tzf "$tar_path" | awk '/(^|\/)crabwise$/ {print; exit}')"
  if [ -z "$entry" ]; then
    echo "archive ${tar_name} missing crabwise binary" >&2
    exit 1
  fi

  mkdir -p "$extract_dir"
  tar -xzf "$tar_path" -C "$extract_dir" "$entry"
  cp "${extract_dir}/${entry}" "$dest"
  chmod +x "$dest"
}

stage_one "darwin" "amd64" "npm/platform/darwin-x64"
stage_one "darwin" "arm64" "npm/platform/darwin-arm64"
stage_one "linux" "amd64" "npm/platform/linux-x64"
stage_one "linux" "arm64" "npm/platform/linux-arm64"

echo "staged release binaries for ${tag}"
