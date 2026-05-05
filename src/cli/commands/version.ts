import { defineCommand } from "citty";
import { printObject } from "../output.js";
import { getGitSha, getVersion } from "../version-info.js";

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
    const version = getVersion();
    const sha = getGitSha();
    if (args.quiet) {
      process.stdout.write(version + "\n");
      return;
    }
    printObject({ version, git_sha: sha ?? "—" }, { json: args.json });
  },
});
