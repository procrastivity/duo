import { defineCommand } from "citty";
import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { printObject } from "../output.js";

const findPackageVersion = (): string => {
  // Resolve relative to this file (works in src/ and dist/).
  const here = dirname(fileURLToPath(import.meta.url));
  const candidates = [
    resolve(here, "../../../package.json"),
    resolve(here, "../../package.json"),
    resolve(here, "../package.json"),
  ];
  for (const path of candidates) {
    try {
      const pkg = JSON.parse(readFileSync(path, "utf8")) as { name?: string; version?: string };
      if (pkg.name && pkg.version) return pkg.version;
    } catch {
      // try next
    }
  }
  return "unknown";
};

const findGitSha = (): string | undefined => {
  try {
    return execSync("git rev-parse --short HEAD", { stdio: ["ignore", "pipe", "ignore"] })
      .toString()
      .trim();
  } catch {
    return undefined;
  }
};

export const versionCommand = defineCommand({
  meta: {
    name: "version",
    description: "Print the Duo version and git sha",
  },
  args: {
    json: { type: "boolean", description: "Emit JSON" },
    quiet: { type: "boolean", alias: "q", description: "Print version only" },
  },
  async run({ args }) {
    const version = findPackageVersion();
    const sha = findGitSha();
    if (args.quiet) {
      process.stdout.write(version + "\n");
      return;
    }
    printObject({ version, git_sha: sha ?? "—" }, { json: args.json });
  },
});
