import { defineCommand } from "citty";
import type { PresetDefinition } from "../../types/presets.js";
import type { SoloAgentTool } from "../../types/solo.js";
import {
  connectSolo,
  handleSoloError,
  EXIT_USER_ERROR,
} from "../connect.js";
import {
  generateDefinitionId,
  readRawConfig,
  writeConfig,
} from "../config-writer.js";
import { writeErr, writeJson, writeOut } from "../output.js";

/**
 * Pure `--agent-tool` selector resolver (D4).
 *
 * Given the live `list_agent_tools` and a user selector, decide which
 * `agent_tool_id` to persist:
 *
 * - **all-digits** selector → treated as an `agent_tool_id`; it must exist in
 *   `tools`. A matched-but-`enabled: false` tool still resolves `ok` (persist
 *   anyway; enable-state is dynamic and owned by a later step) — the caller
 *   inspects `tool.enabled` to decide whether to warn.
 * - **otherwise** → case-insensitive **exact** name match against `tool.name`
 *   (NOT substring, so `codex` matches `Codex` but not `Codex • GPT 5.5`).
 *   Unique → `ok`; more than one → `ambiguous`; none → `notFound`.
 *
 * The function reads nothing and writes nothing — it is directly unit-tested.
 */
export type PresetToolResolution =
  | { readonly ok: true; readonly agent_tool_id: number; readonly tool: SoloAgentTool }
  | { readonly ambiguous: true; readonly candidates: readonly SoloAgentTool[] }
  | { readonly notFound: true };

export const resolvePresetAgentTool = (
  tools: readonly SoloAgentTool[],
  selector: string,
): PresetToolResolution => {
  const trimmed = selector.trim();

  if (/^\d+$/.test(trimmed)) {
    const id = Number(trimmed);
    const tool = tools.find((t) => t.id === id);
    if (tool) {
      return { ok: true, agent_tool_id: tool.id, tool };
    }
    return { notFound: true };
  }

  const wanted = trimmed.toLowerCase();
  const matches = tools.filter((t) => t.name.toLowerCase() === wanted);
  if (matches.length === 1) {
    const tool = matches[0]!;
    return { ok: true, agent_tool_id: tool.id, tool };
  }
  if (matches.length > 1) {
    return { ambiguous: true, candidates: matches };
  }
  return { notFound: true };
};

/** Collect every existing definition id across all presets in a raw config. */
const collectExistingIds = (raw: Record<string, unknown>): string[] => {
  const ids: string[] = [];
  const presets = raw.presets;
  if (presets && typeof presets === "object" && !Array.isArray(presets)) {
    for (const defs of Object.values(presets as Record<string, unknown>)) {
      if (Array.isArray(defs)) {
        for (const def of defs) {
          if (def && typeof def === "object" && typeof (def as { id?: unknown }).id === "string") {
            ids.push((def as { id: string }).id);
          }
        }
      }
    }
  }
  return ids;
};

export interface PresetAddInput {
  readonly name: string;
  readonly agentTool: string;
  readonly extraArgs?: string;
  readonly provider?: string;
}

export type PresetAddResult =
  | {
      readonly status: "written";
      readonly preset: string;
      readonly definition: PresetDefinition;
      readonly tool: SoloAgentTool;
      readonly disabledWarning: boolean;
    }
  | { readonly status: "ambiguous"; readonly selector: string; readonly candidates: readonly SoloAgentTool[] }
  | { readonly status: "not_found"; readonly selector: string; readonly tools: readonly SoloAgentTool[] };

/** Minimal Solo client surface this command needs — eases testing with a fake. */
export interface AgentToolLister {
  listAgentTools(): Promise<SoloAgentTool[]>;
}

/**
 * Core of `duo config preset add`: list tools, resolve the selector (D4), and on
 * success append a new definition (with a generated id, D2) to `presets[name]`
 * via the Task-2 writer (D5: ALWAYS append, duplicates allowed).
 *
 * On `ambiguous` / `not_found` it writes **nothing** and returns the outcome so
 * the caller can print candidates and exit non-zero. Reads/writes config via the
 * writer, which honors `resolveConfigPath()` (test with `DUO_CONFIG`).
 */
export const presetAdd = async (
  client: AgentToolLister,
  input: PresetAddInput,
): Promise<PresetAddResult> => {
  const tools = await client.listAgentTools();
  const resolution = resolvePresetAgentTool(tools, input.agentTool);

  if ("ambiguous" in resolution) {
    return { status: "ambiguous", selector: input.agentTool, candidates: resolution.candidates };
  }
  if ("notFound" in resolution) {
    return { status: "not_found", selector: input.agentTool, tools };
  }

  const raw = readRawConfig();
  const id = generateDefinitionId(collectExistingIds(raw));

  const definition: PresetDefinition = {
    id,
    agent_tool_id: resolution.agent_tool_id,
    ...(input.extraArgs !== undefined && input.extraArgs !== ""
      ? { extra_args: input.extraArgs }
      : {}),
    ...(input.provider !== undefined && input.provider !== ""
      ? { provider: input.provider }
      : {}),
  };

  const presets =
    raw.presets && typeof raw.presets === "object" && !Array.isArray(raw.presets)
      ? (raw.presets as Record<string, PresetDefinition[]>)
      : {};
  const existing = Array.isArray(presets[input.name]) ? presets[input.name]! : [];
  presets[input.name] = [...existing, definition];
  raw.presets = presets;

  writeConfig(raw);

  return {
    status: "written",
    preset: input.name,
    definition,
    tool: resolution.tool,
    disabledWarning: resolution.tool.enabled === false,
  };
};

const formatToolLine = (t: SoloAgentTool): string =>
  `  #${t.id}  ${t.name}${t.enabled === false ? " (disabled)" : ""}`;

const addCommand = defineCommand({
  meta: {
    name: "add",
    description: "Add a preset definition mapping a label to an agent tool",
  },
  args: {
    name: { type: "positional", required: true, description: "Preset label (e.g. builder)" },
    "agent-tool": {
      type: "string",
      required: true,
      description: "Agent tool selector: numeric id or exact tool name",
    },
    "extra-arguments": {
      type: "string",
      description: "Opaque per-launch args appended to the agent command at spawn time",
    },
    provider: { type: "string", description: "Freeform provider label" },
    cwd: { type: "string", description: "Working directory" },
    json: { type: "boolean", description: "Emit JSON" },
    quiet: { type: "boolean", alias: "q", description: "Print only the new definition id" },
  },
  async run({ args }) {
    const name = String(args.name ?? "").trim();
    if (!name) {
      writeErr("A preset name is required, e.g. `duo config preset add builder --agent-tool=Codex`.");
      process.exit(EXIT_USER_ERROR);
    }
    const agentTool = String(args["agent-tool"] ?? "").trim();
    if (!agentTool) {
      writeErr("--agent-tool is required (a numeric agent_tool_id or an exact tool name).");
      process.exit(EXIT_USER_ERROR);
    }

    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    let result: PresetAddResult;
    try {
      result = await presetAdd(client, {
        name,
        agentTool,
        extraArgs: args["extra-arguments"],
        provider: args.provider,
      });
    } catch (err) {
      await dispose();
      handleSoloError(err);
      return;
    }
    await dispose();

    if (result.status === "ambiguous") {
      writeErr(`Ambiguous agent tool "${result.selector}" — ${result.candidates.length} matches:`);
      for (const t of result.candidates) writeErr(formatToolLine(t));
      writeErr("Pass a specific --agent-tool=<id> to disambiguate. Nothing was written.");
      process.exit(EXIT_USER_ERROR);
    }
    if (result.status === "not_found") {
      writeErr(`No agent tool matches "${result.selector}". Available tools:`);
      for (const t of result.tools) writeErr(formatToolLine(t));
      writeErr("Nothing was written.");
      process.exit(EXIT_USER_ERROR);
    }

    const { definition, tool, preset } = result;
    if (result.disabledWarning) {
      writeErr(
        `[warn] agent tool #${tool.id} (${tool.name}) is currently disabled; persisting anyway.`,
      );
    }

    if (args.json) {
      writeJson({
        preset,
        definition,
        agent_tool: { id: tool.id, name: tool.name, enabled: tool.enabled },
      });
      return;
    }
    if (args.quiet) {
      writeOut(definition.id);
      return;
    }
    writeOut(`preset:      ${preset}`);
    writeOut(`id:          ${definition.id}`);
    writeOut(`tool:        ${tool.name} (#${tool.id})`);
    writeOut(`extra_args:  ${definition.extra_args ?? "—"}`);
    writeOut(`provider:    ${definition.provider ?? "—"}`);
  },
});

/**
 * The `preset` subcommand group under `duo config`. Task-3 ships `add`;
 * `list` and `remove` (Tasks 4–5) attach here as siblings.
 */
export const presetCommand = defineCommand({
  meta: {
    name: "preset",
    description: "Manage Duo presets (label → agent tool + extra args + provider)",
  },
  subCommands: {
    add: addCommand,
  },
});
