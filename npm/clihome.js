#!/usr/bin/env node
"use strict";

// Launcher for the bundled clihome Go binaries. This single npm package ships
// every platform's binary under bin/<platform>-<arch>/; this shim picks the one
// matching the host and hands over the terminal so the Bubble Tea TUI runs
// natively.

const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");
const path = require("node:path");

const SUPPORTED = new Set([
  "darwin-arm64",
  "darwin-x64",
  "linux-arm64",
  "linux-x64",
  "win32-x64",
]);

const key = `${process.platform}-${process.arch}`;

if (!SUPPORTED.has(key)) {
  console.error(
    `clihome: unsupported platform "${key}".\n` +
      `Supported: ${[...SUPPORTED].join(", ")}.\n` +
      `Build from source instead: https://github.com/Supavasinan/clihome`
  );
  process.exit(1);
}

const binName = process.platform === "win32" ? "clihome.exe" : "clihome";
const binPath = path.join(__dirname, key, binName);

if (!existsSync(binPath)) {
  console.error(
    `clihome: bundled binary not found at ${binPath}.\n` +
      `Try reinstalling:  npm install -g clihome --force`
  );
  process.exit(1);
}

const result = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(`clihome: failed to launch binary: ${result.error.message}`);
  process.exit(1);
}
if (result.signal) {
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
