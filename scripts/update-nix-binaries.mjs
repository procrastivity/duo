// Refresh nix/prebuilt-binaries.json for a given release tag.
//
// The prebuilt `packages.duo-bin` derivation fetches a standalone binary from
// the GitHub release by URL + sha256. Those hashes only exist once the release
// assets are uploaded (by .github/workflows/release-bin.yml), so this runs
// AFTER a release — the release-bin.yml `update-nix-manifest` job runs it
// automatically and commits the result. Run it by hand only to backfill.
//
// Usage:
//   node scripts/update-nix-binaries.mjs [vX.Y.Z]
//
// With no tag, uses `v<package.json version>`. Pure Node (global fetch +
// node:crypto) — no external tools required, so it runs anywhere CI does.

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";

const REPO = "procrastivity/duo";

// nix system → release asset name
const SYSTEMS = {
  "aarch64-darwin": "duo-darwin-arm64",
  "aarch64-linux": "duo-linux-arm64",
  "x86_64-linux": "duo-linux-x64",
};

const manifestPath = new URL("../nix/prebuilt-binaries.json", import.meta.url);

const pkg = JSON.parse(
  readFileSync(new URL("../package.json", import.meta.url), "utf8"),
);

const tag = process.argv[2] ?? `v${pkg.version}`;
const version = tag.replace(/^v/, "");

// Download the asset and return its Subresource-Integrity hash. This is
// byte-for-byte the same string `nix store prefetch-file` emits: the literal
// "sha256-" prefix followed by base64 of the raw 32-byte digest.
const sriHash = async (url) => {
  const res = await fetch(url, { redirect: "follow" });
  if (!res.ok) {
    throw new Error(`fetch ${url} failed: ${res.status} ${res.statusText}`);
  }
  const buf = Buffer.from(await res.arrayBuffer());
  return `sha256-${createHash("sha256").update(buf).digest("base64")}`;
};

const systems = {};
for (const [system, asset] of Object.entries(SYSTEMS)) {
  const url = `https://github.com/${REPO}/releases/download/${tag}/${asset}`;
  process.stderr.write(`hashing ${asset} …\n`);
  systems[system] = { asset, url, hash: await sriHash(url) };
}

const manifest = { tag, version, systems };
writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + "\n");
process.stderr.write(`wrote nix/prebuilt-binaries.json for ${tag}\n`);
