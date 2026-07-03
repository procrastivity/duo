import { defineCommand } from "citty";
import { connectSolo, handleSoloError, EXIT_USER_ERROR } from "../connect.js";
import { loadConfig } from "../config-loader.js";
import { listPresets } from "../../tools/list-presets.js";
import { resolvePreset } from "../../resolver.js";
import { writeErr, writeJson, writeOut, renderTable } from "../output.js";
import { PresetUnavailableError, UnknownPresetError } from "../../errors.js";

const loadPresets = (cwd: string | undefined) => {
  try {
    return loadConfig({ cwd }).config.presets ?? {};
  } catch (err) {
    writeErr(err instanceof Error ? err.message : String(err));
    process.exit(EXIT_USER_ERROR);
  }
};

const listCommand = defineCommand({
  meta: { name: "list", description: "List configured agent presets" },
  args: {
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const presets = loadPresets(args.cwd);
    const view = listPresets(presets);
    if (args.json) {
      writeJson(view);
      return;
    }
    if (args.quiet) return;
    const names = Object.keys(view).sort();
    if (names.length === 0) {
      writeOut("No presets configured.");
      return;
    }
    const rows = names.map((name) => {
      const p = view[name]!;
      const defs = p.definitions
        .map(
          (d) =>
            `${d.provider ?? "—"}:#${d.agent_tool_id}${d.enabled ? "" : " (off)"}`,
        )
        .join(", ");
      return {
        preset: name,
        available: p.available ? "yes" : "no",
        defs: defs || "—",
      };
    });
    writeOut(
      renderTable(rows, [
        { header: "PRESET", get: (r) => r.preset },
        { header: "AVAILABLE", get: (r) => r.available },
        { header: "DEFINITIONS", get: (r) => r.defs, truncate: 60 },
      ]),
    );
  },
});

const resolveCommand = defineCommand({
  meta: {
    name: "resolve",
    description: "Resolve which agent tool a preset would select",
  },
  args: {
    preset: { type: "positional", required: true, description: "Preset name" },
    "avoid-provider": {
      type: "string",
      description: "Soft-avoid a provider when selecting a definition",
    },
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const presets = loadPresets(args.cwd);
    const presetName = String(args.preset);
    try {
      const resolution = resolvePreset(presets, presetName, {
        avoidProvider: args["avoid-provider"],
      });
      if (args.json) {
        writeJson(resolution);
      } else if (args.quiet) {
        writeOut(String(resolution.agent_tool_id));
      } else {
        writeOut(`preset:      ${resolution.preset_requested}`);
        writeOut(`preset_used: ${resolution.preset_used}`);
        writeOut(`tool_id:     ${resolution.agent_tool_id}`);
        if (resolution.provider !== undefined)
          writeOut(`provider:    ${resolution.provider}`);
        if (resolution.extra_args.length > 0)
          writeOut(`extra_args:  ${resolution.extra_args.join(" ")}`);
      }
    } catch (e) {
      if (e instanceof UnknownPresetError) {
        writeErr(`Unknown preset "${e.preset}".`);
        process.exit(EXIT_USER_ERROR);
      }
      if (e instanceof PresetUnavailableError) {
        writeErr(e.message);
        process.exit(EXIT_USER_ERROR);
      }
      throw e;
    }
  },
});

const launchCommand = defineCommand({
  meta: { name: "launch", description: "Launch an agent process for a preset" },
  args: {
    preset: { type: "positional", required: true, description: "Preset name" },
    name: { type: "string", description: "Process name" },
    "project-id": { type: "string", description: "Override Solo project ID" },
    "avoid-provider": {
      type: "string",
      description: "Soft-avoid a provider when selecting a definition",
    },
    prompt: { type: "string", description: "Bootstrap prompt delivered as the agent's first message" },
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const presetName = String(args.preset);
    let projectId: number | undefined;
    if (args["project-id"]) {
      const n = Number(args["project-id"]);
      if (!Number.isInteger(n) || n < 0) {
        writeErr(`--project-id must be a non-negative integer`);
        process.exit(EXIT_USER_ERROR);
      }
      projectId = n;
    }

    const { client, config, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const presets = config.config.presets ?? {};
      let resolution;
      try {
        resolution = resolvePreset(presets, presetName, {
          avoidProvider: args["avoid-provider"],
        });
      } catch (e) {
        if (e instanceof UnknownPresetError) {
          writeErr(`Unknown preset "${e.preset}".`);
          process.exit(EXIT_USER_ERROR);
        }
        if (e instanceof PresetUnavailableError) {
          writeErr(e.message);
          process.exit(EXIT_USER_ERROR);
        }
        throw e;
      }

      const spawnArgs: {
        kind: "agent";
        agent_tool_id: number;
        name?: string;
        project_id?: number;
        prompt?: string;
        extra_args?: string[];
      } = {
        kind: "agent",
        agent_tool_id: resolution.agent_tool_id,
      };
      if (args.name) spawnArgs.name = args.name;
      if (projectId !== undefined) spawnArgs.project_id = projectId;
      if (args.prompt) spawnArgs.prompt = args.prompt;
      if (resolution.extra_args.length > 0)
        spawnArgs.extra_args = resolution.extra_args;

      const spawned = await client.spawnProcess(spawnArgs);
      const effectiveProjectId = projectId ?? client.projectId;
      const url =
        effectiveProjectId !== undefined
          ? `solo://proj/${effectiveProjectId}/process/duo-agent--${spawned.process_id}`
          : undefined;
      const result = {
        process_id: spawned.process_id,
        name: spawned.name,
        preset: presetName,
        agent_tool_id: resolution.agent_tool_id,
        extra_args: resolution.extra_args,
        ...(resolution.provider !== undefined && { provider: resolution.provider }),
        project_id: effectiveProjectId,
        ...(url !== undefined && { url }),
      };
      if (args.json) {
        writeJson(result);
      } else if (args.quiet) {
        writeOut(String(result.process_id));
      } else {
        writeOut(`process_id:  ${result.process_id}`);
        writeOut(`name:        ${result.name}`);
        writeOut(`preset:      ${result.preset}`);
        writeOut(`tool_id:     ${result.agent_tool_id}`);
        if (result.provider !== undefined)
          writeOut(`provider:    ${result.provider}`);
        if (result.extra_args.length > 0)
          writeOut(`extra_args:  ${result.extra_args.join(" ")}`);
        if (result.project_id !== undefined)
          writeOut(`project_id:  ${result.project_id}`);
        if (url !== undefined) writeOut(`url:         ${url}`);
      }
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

export const agentCommand = defineCommand({
  meta: { name: "agent", description: "Manage Duo agent presets" },
  subCommands: {
    list: listCommand,
    resolve: resolveCommand,
    launch: launchCommand,
  },
});
