#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 vX.Y.Z" >&2
  exit 1
fi

input_version="$1"
version="${input_version#v}"

if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?(\+([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]]; then
  echo "invalid version: ${input_version} (expected vX.Y.Z or X.Y.Z with optional prerelease/build)" >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

node - "$version" "$repo_root" <<'EOF'
const fs = require("node:fs");
const path = require("node:path");

const version = process.argv[2];
const root = process.argv[3];
const files = [
  "npm/crabwise/package.json",
  "npm/platform/darwin-x64/package.json",
  "npm/platform/darwin-arm64/package.json",
  "npm/platform/linux-x64/package.json",
  "npm/platform/linux-arm64/package.json"
];

for (const rel of files) {
  const file = path.join(root, rel);
  const data = JSON.parse(fs.readFileSync(file, "utf8"));
  data.version = version;
  if (data.optionalDependencies) {
    for (const key of Object.keys(data.optionalDependencies)) {
      data.optionalDependencies[key] = version;
    }
  }
  fs.writeFileSync(file, `${JSON.stringify(data, null, 2)}\n`);
}
EOF

echo "set npm package versions to ${version}"
