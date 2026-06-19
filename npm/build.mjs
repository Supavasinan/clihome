#!/usr/bin/env node
// Build all npm artifacts for clihome:
//   - one per-platform package (clihome-<os>-<arch>) holding a native Go binary
//   - the launcher package (clihome) whose bin shim execs the right binary
//
// Output goes to npm/dist/. Run locally to smoke-test, or in CI before publish.
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

// node platform/arch  <->  Go GOOS/GOARCH. Keep in sync with npm/clihome.js.
const TARGETS = [
  { os: "darwin", cpu: "arm64", goos: "darwin", goarch: "arm64" },
  { os: "darwin", cpu: "x64", goos: "darwin", goarch: "amd64" },
  { os: "linux", cpu: "arm64", goos: "linux", goarch: "arm64" },
  { os: "linux", cpu: "x64", goos: "linux", goarch: "amd64" },
  { os: "win32", cpu: "x64", goos: "windows", goarch: "amd64" },
];

function resolveVersion() {
  const argIdx = process.argv.indexOf("--version");
  if (argIdx !== -1 && process.argv[argIdx + 1]) {
    return process.argv[argIdx + 1].replace(/^v/, "");
  }
  if (process.env.VERSION) return process.env.VERSION.replace(/^v/, "");
  const tmpl = JSON.parse(
    readFileSync(join(here, "package.template.json"), "utf8")
  );
  return tmpl.version;
}

const version = resolveVersion();
const template = JSON.parse(
  readFileSync(join(here, "package.template.json"), "utf8")
);
const launcherName = template.name;

console.log(`Building ${launcherName} v${version}`);
rmSync(distDir, { recursive: true, force: true });
mkdirSync(distDir, { recursive: true });

const optionalDependencies = {};

for (const t of TARGETS) {
  const pkgName = `${launcherName}-${t.os}-${t.cpu}`;
  const pkgDir = join(distDir, pkgName);
  const binDir = join(pkgDir, "bin");
  const binName = t.os === "win32" ? "clihome.exe" : "clihome";
  mkdirSync(binDir, { recursive: true });

  process.stdout.write(`  ${t.goos}/${t.goarch} -> ${pkgName} … `);
  execFileSync(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags",
      `-s -w -X main.version=${version}`,
      "-o",
      join(binDir, binName),
      "./cmd/clihome",
    ],
    {
      cwd: repoRoot,
      stdio: "inherit",
      env: { ...process.env, GOOS: t.goos, GOARCH: t.goarch, CGO_ENABLED: "0" },
    }
  );
  if (t.os !== "win32") chmodSync(join(binDir, binName), 0o755);

  writeFileSync(
    join(pkgDir, "package.json"),
    JSON.stringify(
      {
        name: pkgName,
        version,
        description: `clihome native binary for ${t.os}-${t.cpu}.`,
        repository: template.repository,
        license: template.license,
        os: [t.os],
        cpu: [t.cpu],
        files: ["bin"],
        preferUnplugged: true,
      },
      null,
      2
    ) + "\n"
  );
  writeFileSync(
    join(pkgDir, "README.md"),
    `# ${pkgName}\n\nNative \`clihome\` binary for ${t.os}-${t.cpu}.\n` +
      `This is an internal platform package — install [\`clihome\`](https://www.npmjs.com/package/clihome) instead.\n`
  );
  cpSync(join(repoRoot, "LICENSE"), join(pkgDir, "LICENSE"));

  optionalDependencies[pkgName] = version;
  console.log("ok");
}

// Launcher package.
const launcherDir = join(distDir, launcherName);
mkdirSync(join(launcherDir, "bin"), { recursive: true });

const launcherPkg = { ...template, version, optionalDependencies };
writeFileSync(
  join(launcherDir, "package.json"),
  JSON.stringify(launcherPkg, null, 2) + "\n"
);
cpSync(join(here, "clihome.js"), join(launcherDir, "bin", "clihome.js"));
chmodSync(join(launcherDir, "bin", "clihome.js"), 0o755);
cpSync(join(here, "README.md"), join(launcherDir, "README.md"));
cpSync(join(repoRoot, "LICENSE"), join(launcherDir, "LICENSE"));

console.log(`\nDone. Artifacts in ${distDir}`);
console.log(`Publish order: platform packages first, then ${launcherName}.`);
