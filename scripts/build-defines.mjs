import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";

// Returns the build-time `--define` substitutions for both the esbuild Node
// bundle and `bun build --compile`. Empty `gitSha` means the build is running
// outside a git checkout (e.g. from a source tarball); the runtime helper
// treats that as authoritative — see `src/cli/version-info.ts`.
export const readBuildDefines = () => {
  const pkg = JSON.parse(
    readFileSync(new URL("../package.json", import.meta.url), "utf8"),
  );
  let gitSha = "";
  try {
    gitSha = execSync("git rev-parse --short HEAD", {
      stdio: ["ignore", "pipe", "ignore"],
    })
      .toString()
      .trim();
  } catch {
    // outside a git checkout
  }
  return { version: pkg.version, gitSha };
};
