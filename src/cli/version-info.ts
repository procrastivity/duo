import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// `__DUO_VERSION__` and `__DUO_GIT_SHA__` are substituted at build time via
// `--define` so the compiled binary and Node bundle report the version and
// commit they were built from — not whatever package.json or git repo the
// user happens to be running them inside. Synthetic globals (rather than
// `process.env`) so a stray env var can't change the output. They can still
// be overridden by deliberately writing to `globalThis` (which the unit
// tests in `version-info.test.ts` rely on); this is not a security boundary.
declare const __DUO_VERSION__: string | undefined;
declare const __DUO_GIT_SHA__: string | undefined;

export const getVersion = (): string => {
  if (typeof __DUO_VERSION__ !== "undefined") return __DUO_VERSION__;
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
  // When `__DUO_GIT_SHA__` is injected at build time it's authoritative,
  // even when empty: an empty value means "build had no SHA available",
  // not "fall back to whatever git repo the user happens to be in".
  if (typeof __DUO_GIT_SHA__ !== "undefined") return __DUO_GIT_SHA__ || undefined;
  try {
    return execSync("git rev-parse --short HEAD", { stdio: ["ignore", "pipe", "ignore"] })
      .toString()
      .trim();
  } catch {
    return undefined;
  }
};
