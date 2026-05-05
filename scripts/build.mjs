import * as esbuild from "esbuild";
import { chmod } from "fs/promises";

// The banner injects a createRequire shim so CJS dependencies (like cross-spawn,
// pulled in by @modelcontextprotocol/sdk and execa) can resolve Node built-ins
// (child_process, etc.) via require() inside the ESM bundle.
const banner = [
  "#!/usr/bin/env node",
  'import { createRequire } from "module";',
  "var require = createRequire(import.meta.url);",
].join("\n");

await esbuild.build({
  entryPoints: ["src/index.ts"],
  bundle: true,
  platform: "node",
  format: "esm",
  target: "node24",
  outfile: "dist/duo.mjs",
  banner: { js: banner },
  legalComments: "none",
});

await chmod("dist/duo.mjs", 0o755);
