import { spawn } from "node:child_process";
import { readBuildDefines } from "./build-defines.mjs";

const target = process.argv[2];
const outfile = process.argv[3];

if (!target || !outfile) {
  console.error("usage: build-bin.mjs <bun-target> <outfile>");
  process.exit(1);
}

const { version, gitSha } = readBuildDefines();

const child = spawn(
  "bun",
  [
    "build",
    "src/index.ts",
    "--compile",
    `--target=${target}`,
    `--outfile=${outfile}`,
    "--define",
    `__DUO_VERSION__=${JSON.stringify(version)}`,
    "--define",
    `__DUO_GIT_SHA__=${JSON.stringify(gitSha)}`,
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
