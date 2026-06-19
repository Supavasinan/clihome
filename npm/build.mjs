#!/usr/bin/env node
// Build the single clihome npm package: one package bundling a native Go binary
// for every supported platform under bin/<platform>-<arch>/, plus the launcher
// shim that execs the right one. Output goes to npm/dist/clihome/.
//
//   node npm/build.mjs --version 1.2.3      # explicit version
//   VERSION=1.2.3 node npm/build.mjs        # via env (leading "v" is stripped)
//   node npm/build.mjs                      # falls back to template version

import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdirSync,
  rmSync,
  writeFileSync,
  chmodSync,
  readFileSync,
} from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..");
const distDir = join(here, "dist");

// node platform/arch (the bin/ subdir name)  <->  Go GOOS/GOARCH.
// Keep the keys in sync with the SUPPORTED set in npm/clihome.js.
const TARGETS = [
  { key: "darwin-arm64", goos: "darwin", goarch: "arm64" },
  { key: "darwin-x64", goos: "darwin", goarch: "amd64" },
  { key: "linux-arm64", goos: "linux", goarch: "arm64" },
  { key: "linux-x64", goos: "linux", goarch: "amd64" },
  { key: "win32-x64", goos: "windows", goarch: "amd64" },
];

function resolveVersion() {
  const argIdx = process.argv.indexOf("--version");
  if (argIdx !== -1 && process.argv[argIdx + 1]) {
    return process.argv[argIdx + 1].replace(/^v/, "");
  }
  if (process.env.VERSION) return process.env.VERSION.replace(/^v/, "");
  return JSON.parse(readFileSync(join(here, "package.template.json"), "utf8")).version;
}

const version = resolveVersion();
const template = JSON.parse(
  readFileSync(join(here, "package.template.json"), "utf8")
);

console.log(`Building ${template.name} v${version} (single bundled package)`);
rmSync(distDir, { recursive: true, force: true });

const pkgDir = join(distDir, template.name);
const binDir = join(pkgDir, "bin");
mkdirSync(binDir, { recursive: true });

for (const t of TARGETS) {
  const isWin = t.goos === "windows";
  const outDir = join(binDir, t.key);
  mkdirSync(outDir, { recursive: true });
  const out = join(outDir, isWin ? "clihome.exe" : "clihome");

  process.stdout.write(`  ${t.goos}/${t.goarch} -> bin/${t.key} … `);
  execFileSync(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags",
      `-s -w -X main.version=${version}`,
      "-o",
      out,
      "./cmd/clihome",
    ],
    {
      cwd: repoRoot,
      stdio: "inherit",
      env: { ...process.env, GOOS: t.goos, GOARCH: t.goarch, CGO_ENABLED: "0" },
    }
  );
  if (!isWin) chmodSync(out, 0o755);
  console.log("ok");
}

// Single package manifest from the template (no os/cpu, no platform deps).
const pkg = { ...template, version };
delete pkg.optionalDependencies;
writeFileSync(join(pkgDir, "package.json"), JSON.stringify(pkg, null, 2) + "\n");

cpSync(join(here, "clihome.js"), join(binDir, "clihome.js"));
chmodSync(join(binDir, "clihome.js"), 0o755);
cpSync(join(here, "README.md"), join(pkgDir, "README.md"));
cpSync(join(repoRoot, "LICENSE"), join(pkgDir, "LICENSE"));

console.log(`\nDone. Package in ${pkgDir}`);
