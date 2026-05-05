import * as esbuild from "esbuild";
import { chmod } from "node:fs/promises";
import { readBuildDefines } from "./build-defines.mjs";

// The banner injects a createRequire shim so CJS dependencies (like cross-spawn,
// pulled in by @modelcontextprotocol/sdk and execa) can resolve Node built-ins
// (child_process, etc.) via require() inside the ESM bundle.
const banner = [
  "#!/usr/bin/env node",
  'import { createRequire } from "module";',
  "var require = createRequire(import.meta.url);",
].join("\n");

const { version, gitSha } = readBuildDefines();

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
    __DUO_VERSION__: JSON.stringify(version),
    __DUO_GIT_SHA__: JSON.stringify(gitSha),
  },
});

await chmod("dist/duo.mjs", 0o755);
