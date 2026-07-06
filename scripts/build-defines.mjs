import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// Returns the build-time `--define` substitutions for both the esbuild Node
// bundle and `bun build --compile`. Empty `gitSha` means the build is running
// outside a git checkout (e.g. from a source tarball); the runtime helper
// treats that as authoritative — see `src/cli/version-info.ts`.
//
// `DUO_GIT_SHA` takes precedence when set non-empty: the Nix from-source build
// runs in a sandbox with no `.git`, so `git rev-parse` yields nothing and
// `duo version` would print `—`. The flake injects the sha via this env var
// (`packages.duo` sets `DUO_GIT_SHA = self.shortRev or self.dirtyShortRev`).
// When it is unset/empty we fall back to `git rev-parse`, anchored to this
// script's repo (one level above `scripts/`) not the caller's CWD, so launching
// the build from another directory still picks up the Duo source SHA.
export const readBuildDefines = () => {
  const pkg = JSON.parse(
    readFileSync(new URL("../package.json", import.meta.url), "utf8"),
  );
  const repoRoot = fileURLToPath(new URL("..", import.meta.url));
  let gitSha = (process.env.DUO_GIT_SHA ?? "").trim();
  if (!gitSha) {
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
  }
  return { version: pkg.version, gitSha };
};
