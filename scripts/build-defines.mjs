import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// Returns the build-time `--define` substitutions for both the esbuild Node
// bundle and `bun build --compile`. Empty `gitSha` means the build is running
// outside a git checkout (e.g. from a source tarball); the runtime helper
// treats that as authoritative — see `src/cli/version-info.ts`.
//
// `git rev-parse` runs anchored to this script's repo (one level above
// `scripts/`), not the caller's CWD, so launching the build from another
// directory still picks up the Duo source SHA.
export const readBuildDefines = () => {
  const pkg = JSON.parse(
    readFileSync(new URL("../package.json", import.meta.url), "utf8"),
  );
  const repoRoot = fileURLToPath(new URL("..", import.meta.url));
  let gitSha = "";
  try {
    gitSha = execSync("git rev-parse --short HEAD", {
      stdio: ["ignore", "pipe", "ignore"],
      cwd: repoRoot,
    })
      .toString()
      .trim();
  } catch {
    // outside a git checkout
  }
  return { version: pkg.version, gitSha };
};
