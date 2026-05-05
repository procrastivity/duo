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
    `process.env.DUO_VERSION=${JSON.stringify(pkg.version)}`,
  ],
  { stdio: "inherit" },
);

child.on("exit", (code, signal) => {
  if (signal) process.kill(process.pid, signal);
  else process.exit(code ?? 1);
});
