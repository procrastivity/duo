// Recompute packages.duo's npmDepsHash in flake.nix from package-lock.json.
//
// buildNpmPackage pins a fixed-output hash of the vendored npm dependencies.
// That hash changes whenever package-lock.json changes — including the plain
// version bump written into it at release time. Nothing regenerated it
// post-release, so building `#duo` from any ref at/after such a release failed
// with a hash mismatch. The release-bin.yml `refresh-npm-hash` job runs this
// AFTER a tag to keep the from-source build hash in sync.
//
// Usage:
//   node scripts/update-nix-npm-hash.mjs
//
// Requires `prefetch-npm-deps` on PATH (e.g. `nix run nixpkgs#prefetch-npm-deps`
// or a `nix develop` shell). Exits non-zero if the tool is missing so CI fails
// loudly rather than silently leaving a stale hash.

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";

const flakePath = new URL("../flake.nix", import.meta.url);
const lockPath = new URL("../package-lock.json", import.meta.url);

const hash = execFileSync(
  "prefetch-npm-deps",
  [new URL(lockPath).pathname],
  { encoding: "utf8" },
).trim();

if (!/^sha256-[A-Za-z0-9+/]+=*$/.test(hash)) {
  throw new Error(`prefetch-npm-deps returned an unexpected value: ${hash}`);
}

const flake = readFileSync(flakePath, "utf8");
const line = /(\bnpmDepsHash\s*=\s*)"[^"]*"/;
if (!line.test(flake)) {
  throw new Error("could not find npmDepsHash assignment in flake.nix");
}

const updated = flake.replace(line, `$1"${hash}"`);
if (updated === flake) {
  process.stderr.write(`npmDepsHash already up to date (${hash})\n`);
} else {
  writeFileSync(flakePath, updated);
  process.stderr.write(`updated npmDepsHash → ${hash}\n`);
}
