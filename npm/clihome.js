#!/usr/bin/env node
"use strict";

// Thin launcher for the `clihome` Go binary.
//
// The actual binaries are shipped as per-platform optionalDependencies
// (clihome-darwin-arm64, clihome-linux-x64, ...). npm installs only the one
// matching the host's os/cpu, and this shim resolves it and hands over the
// terminal so the Bubble Tea TUI runs natively.

const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");
const path = require("node:path");

// node platform-arch  ->  platform package name
const PACKAGES = {
  "darwin-arm64": "clihome-darwin-arm64",
  "darwin-x64": "clihome-darwin-x64",
  "linux-arm64": "clihome-linux-arm64",
  "linux-x64": "clihome-linux-x64",
  "win32-x64": "clihome-windows-x64",
};

const key = `${process.platform}-${process.arch}`;
const pkg = PACKAGES[key];

if (!pkg) {
  console.error(
    `clihome: unsupported platform "${key}".\n` +
      `Supported: ${Object.keys(PACKAGES).join(", ")}.\n` +
      `Build from source instead: https://github.com/Supavasinan/clihome`
  );
  process.exit(1);
}

const binName = process.platform === "win32" ? "clihome.exe" : "clihome";

let binPath = null;
try {
  // Resolve the platform package's manifest, then the binary next to it.
  const manifest = require.resolve(`${pkg}/package.json`);
  binPath = path.join(path.dirname(manifest), "bin", binName);
} catch {
  binPath = null;
}

if (!binPath || !existsSync(binPath)) {
  console.error(
    `clihome: the platform package "${pkg}" is not installed.\n` +
      `This usually means optionalDependencies were skipped during install.\n` +
      `Reinstall with:  npm install -g clihome --force`
  );
  process.exit(1);
}

const result = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(`clihome: failed to launch binary: ${result.error.message}`);
  process.exit(1);
}

// Mirror the native exit code (signal -> 128+signal, mirroring shell convention).
if (result.signal) {
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
