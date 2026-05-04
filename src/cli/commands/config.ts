import { defineCommand } from "citty";
import { loadConfig, resolveConfigPath } from "../config-loader.js";
import { writeErr, writeJson, writeOut } from "../output.js";
import { EXIT_USER_ERROR } from "../connect.js";
import { stringify as stringifyYaml } from "yaml";

const showCommand = defineCommand({
  meta: {
    name: "show",
    description: "Print the effective Duo config",
  },
  args: {
    cwd: { type: "string", description: "Working directory" },
    json: { type: "boolean", description: "Emit JSON" },
  },
  async run({ args }) {
    try {
      const loaded = loadConfig({ cwd: args.cwd });
      if (args.json) {
        writeJson({
          config_path: loaded.configPath,
          policy_path: loaded.policyPath,
          config: loaded.config,
        });
      } else {
        writeOut(stringifyYaml(loaded.config));
      }
    } catch (err) {
      writeErr(err instanceof Error ? err.message : String(err));
      process.exit(EXIT_USER_ERROR);
    }
  },
});

const pathCommand = defineCommand({
  meta: {
    name: "path",
    description: "Print the path of the loaded duo.config.yaml",
  },
  args: {
    cwd: { type: "string", description: "Working directory" },
    json: { type: "boolean", description: "Emit JSON" },
    quiet: { type: "boolean", alias: "q", description: "Print bare path" },
  },
  async run({ args }) {
    const cwd = args.cwd ?? process.cwd();
    let configPath = resolveConfigPath();
    let policyPath: string | null = null;
    try {
      const loaded = loadConfig({ cwd });
      configPath = loaded.configPath;
      policyPath = loaded.policyPath;
    } catch {
      // Fall through with the resolved (possibly missing) path.
    }
    if (args.quiet) {
      writeOut(configPath);
      return;
    }
    if (args.json) {
      writeJson({ config_path: configPath, policy_path: policyPath });
      return;
    }
    writeOut(`config: ${configPath}`);
    writeOut(`policy: ${policyPath ?? "—"}`);
  },
});

export const configCommand = defineCommand({
  meta: {
    name: "config",
    description: "Inspect Duo configuration",
  },
  subCommands: {
    show: showCommand,
    path: pathCommand,
  },
});
