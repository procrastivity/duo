import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";

const target = process.argv[2];
const outfile = process.argv[3];

if (!target || !outfile) {
  console.error("usage: build-bin.mjs <bun-target> <outfile>");
  process.exit(1);
}

const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));

const child = spawn(
  "bun",
  [
    "build",
    "src/index.ts",
    "--compile",
    `--target=${target}`,
    `--outfile=${outfile}`,
    "--define",
    `__DUO_VERSION__=${JSON.stringify(pkg.version)}`,
  ],
  { stdio: "inherit" },
);

child.on("error", (err) => {
  if (err.code === "ENOENT") {
    console.error("build-bin: 'bun' not found in PATH. Install bun to build the compiled binary.");
  } else {
    console.error(`build-bin: failed to spawn bun: ${err.message}`);
  }
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) process.kill(process.pid, signal);
  else process.exit(code ?? 1);
});
