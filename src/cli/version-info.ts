import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// `process.env.DUO_VERSION` is replaced at build time via `--define` for the
// Bun-compiled binary, where the package.json walk below cannot reach the
// real filesystem (sources live inside Bun's embedded `$bunfs`).
export const getVersion = (): string => {
  const injected = process.env.DUO_VERSION;
  if (injected) return injected;
  const here = dirname(fileURLToPath(import.meta.url));
  for (const path of [
    resolve(here, "../../package.json"),
    resolve(here, "../package.json"),
    resolve(here, "../../../package.json"),
  ]) {
    try {
      const pkg = JSON.parse(readFileSync(path, "utf8")) as { name?: string; version?: string };
      if (pkg.name && pkg.version) return pkg.version;
    } catch {
      // try next
    }
  }
  return "unknown";
};

export const getGitSha = (): string | undefined => {
  try {
    return execSync("git rev-parse --short HEAD", { stdio: ["ignore", "pipe", "ignore"] })
      .toString()
      .trim();
  } catch {
    return undefined;
  }
};
