#!/usr/bin/env node
"use strict";

const path = require("node:path");
const fs = require("node:fs");

const PLATFORMS = {
  darwin: { x64: "@crabwise/darwin-x64", arm64: "@crabwise/darwin-arm64" },
  linux: { x64: "@crabwise/linux-x64", arm64: "@crabwise/linux-arm64" },
};

function getBinaryPath() {
  const platformMap = PLATFORMS[process.platform];
  if (!platformMap) {
    console.error(`unsupported platform: ${process.platform}`);
    process.exit(1);
  }
  const pkg = platformMap[process.arch];
  if (!pkg) {
    console.error(`unsupported arch: ${process.arch}`);
    process.exit(1);
  }
  let pkgDir;
  try {
    pkgDir = path.dirname(require.resolve(`${pkg}/package.json`));
  } catch {
    console.error(`platform package ${pkg} not installed`);
    process.exit(1);
  }
  const bin = path.join(pkgDir, "bin", "crabwise");
  if (!fs.existsSync(bin)) {
    console.error(`binary not found: ${bin}`);
    process.exit(1);
  }
  return bin;
}

const bin = getBinaryPath();
const result = require("node:child_process").spawnSync(bin, process.argv.slice(2), {
  stdio: "inherit",
});
process.exit(result.status ?? 1);
