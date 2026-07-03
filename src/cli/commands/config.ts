import { defineCommand } from "citty";
import { loadConfig, resolveConfigPath } from "../config-loader.js";
import { writeErr, writeJson, writeOut, printResult } from "../output.js";
import { EXIT_USER_ERROR } from "../connect.js";
import { stringify as stringifyYaml } from "yaml";
import { isValidProviderLabel } from "../../state/paths.js";
import { listProviders, setProviderEnabled } from "../../state/providers.js";
import { presetCommand } from "./preset.js";

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
    try {
      const loaded = loadConfig({ cwd });
      configPath = loaded.configPath;
    } catch {
      // Fall through with the resolved (possibly missing) path.
    }
    if (args.quiet) {
      writeOut(configPath);
      return;
    }
    if (args.json) {
      writeJson({ config_path: configPath });
      return;
    }
    writeOut(`config: ${configPath}`);
  },
});

// enable/disable share the same shape; only the target boolean differs. Both are
// OFFLINE — pure filesystem I/O, no Solo connection.
const providerToggleCommand = (verb: "enable" | "disable") =>
  defineCommand({
    meta: {
      name: verb,
      description: `${verb === "enable" ? "Enable" : "Disable"} a provider`,
    },
    args: {
      label: { type: "positional", required: true, description: "Provider label" },
      json: { type: "boolean", description: "Emit JSON" },
      quiet: { type: "boolean", alias: "q", description: "Suppress human output" },
    },
    async run({ args }) {
      const label = String(args.label ?? "");
      if (!isValidProviderLabel(label)) {
        writeErr(
          `Invalid provider label ${JSON.stringify(label)}. Provider labels must ` +
            `match ^[A-Za-z0-9._-]+$ and cannot be "", ".", "..", or contain a ` +
            `path separator.`,
        );
        process.exit(EXIT_USER_ERROR);
      }
      const enabled = verb === "enable";
      setProviderEnabled(label, enabled);
      if (args.json) {
        writeJson({ provider: label, enabled });
        return;
      }
      if (args.quiet) return;
      writeOut(`provider:  ${label}`);
      writeOut(`status:    ${enabled ? "enabled" : "disabled"}`);
    },
  });

const providerListCommand = defineCommand({
  meta: {
    name: "list",
    description: "List providers and their enabled status",
  },
  args: {
    json: { type: "boolean", description: "Emit JSON" },
    quiet: { type: "boolean", alias: "q", description: "Print bare provider names" },
  },
  async run({ args }) {
    printResult(
      listProviders(),
      [
        { header: "PROVIDER", get: (r) => r.provider },
        { header: "STATUS", get: (r) => (r.enabled ? "enabled" : "disabled") },
      ],
      { json: args.json, quiet: args.quiet, quietField: (r) => r.provider },
    );
  },
});

const providerCommand = defineCommand({
  meta: {
    name: "provider",
    description: "Manage provider enabled-state (offline; XDG state files)",
  },
  subCommands: {
    enable: providerToggleCommand("enable"),
    disable: providerToggleCommand("disable"),
    list: providerListCommand,
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
    preset: presetCommand,
    provider: providerCommand,
  },
});
