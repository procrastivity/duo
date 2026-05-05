import * as esbuild from "esbuild";
import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { chmod } from "node:fs/promises";

// The banner injects a createRequire shim so CJS dependencies (like cross-spawn,
// pulled in by @modelcontextprotocol/sdk and execa) can resolve Node built-ins
// (child_process, etc.) via require() inside the ESM bundle.
const banner = [
  "#!/usr/bin/env node",
  'import { createRequire } from "module";',
  "var require = createRequire(import.meta.url);",
].join("\n");

const pkg = JSON.parse(
  readFileSync(new URL("../package.json", import.meta.url), "utf8"),
);

// Capture the Duo source SHA at build time so the published bundle reports
// the commit it was built from, not whatever git repo the user happens to be
// running it inside. Empty string when building outside a git checkout.
const gitSha = (() => {
  try {
    return execSync("git rev-parse --short HEAD", {
      stdio: ["ignore", "pipe", "ignore"],
    })
      .toString()
      .trim();
  } catch {
    return "";
  }
})();

await esbuild.build({
  entryPoints: ["src/index.ts"],
  bundle: true,
  platform: "node",
  format: "esm",
  target: "node24",
  outfile: "dist/duo.mjs",
  banner: { js: banner },
  legalComments: "none",
  define: {
    __DUO_VERSION__: JSON.stringify(pkg.version),
    __DUO_GIT_SHA__: JSON.stringify(gitSha),
  },
});

await chmod("dist/duo.mjs", 0o755);
