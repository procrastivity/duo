import { defineCommand } from "citty";
import type { PresetDefinition, Presets } from "../../types/presets.js";
import { PresetsSchema } from "../../types/presets.js";
import type { SoloAgentTool } from "../../types/solo.js";
import { SoloClient } from "../../solo-client.js";
import { StdioTransport } from "../../transport/stdio.js";
import { resolveTransportCommand } from "../../transport/resolve-command.js";
import {
  connectSolo,
  handleSoloError,
  EXIT_USER_ERROR,
} from "../connect.js";
import { loadConfig } from "../config-loader.js";
import {
  generateDefinitionId,
  readRawConfig,
  writeConfig,
} from "../config-writer.js";
import { writeErr, writeJson, writeOut, renderTable, type Column } from "../output.js";

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
 * Read the `presets` section OFFLINE (D7) — no Solo required. Reuses the Task-2
 * raw reader and the Task-1 {@link PresetsSchema}, so a hand-edited config with a
 * malformed `presets` block fails with a readable Zod message rather than
 * rendering garbage. An absent section is simply an empty map.
 */
export const readPresets = (): Presets => {
  const raw = readRawConfig();
  if (raw.presets === undefined) return {};
  return PresetsSchema.parse(raw.presets);
};

/** One flattened `(preset, definition)` pair, table-ready. */
export interface PresetRow {
  readonly preset: string;
  readonly id: string;
  readonly agent_tool_id: number;
  /** Display for the tool column: `Name (#4)` enriched, `#4` offline, `(unknown tool) (#4)` when the id no longer resolves (OQ3). */
  readonly tool: string;
  readonly extra_args: string;
  readonly provider: string;
}

/**
 * Render the tool column (D7 / OQ2 / OQ3):
 * - no live map (Solo unreachable / not consulted) → `#<id>` (id-only).
 * - live map present, id resolves → `<name> (#<id>)`.
 * - live map present, id absent → `(unknown tool) (#<id>)`.
 */
const toolDisplay = (id: number, names?: ReadonlyMap<number, string> | null): string => {
  if (!names) return `#${id}`;
  const name = names.get(id);
  return name === undefined ? `(unknown tool) (#${id})` : `${name} (#${id})`;
};

/**
 * Pure, offline view builder over an already-loaded presets map. Optionally
 * filters to a single preset and enriches the tool column from a live
 * `agent_tool_id → name` map. Directly unit-tested — reads/spawns nothing.
 */
export const buildPresetView = (
  presets: Presets,
  opts: { readonly filter?: string; readonly toolNames?: ReadonlyMap<number, string> | null } = {},
): { readonly presets: Presets; readonly rows: PresetRow[]; readonly filterMissing: boolean } => {
  const filter = opts.filter?.trim();
  let selected: Presets = presets;
  let filterMissing = false;
  if (filter) {
    if (Object.prototype.hasOwnProperty.call(presets, filter)) {
      selected = { [filter]: presets[filter]! };
    } else {
      selected = {};
      filterMissing = true;
    }
  }

  const rows: PresetRow[] = [];
  for (const [name, defs] of Object.entries(selected)) {
    for (const def of defs) {
      rows.push({
        preset: name,
        id: def.id,
        agent_tool_id: def.agent_tool_id,
        tool: toolDisplay(def.agent_tool_id, opts.toolNames),
        extra_args: def.extra_args ?? "—",
        provider: def.provider ?? "—",
      });
    }
  }
  return { presets: selected, rows, filterMissing };
};

/**
 * Best-effort live tool-name lookup for `list` enrichment (D7 / OQ2). Attempts a
 * Solo connection and `listAgentTools`, returning an `agent_tool_id → name` map
 * on success. On ANY failure (no/invalid config, transport command missing,
 * connect refused, RPC error) it returns `null` so the caller degrades to
 * id-only output. This NEVER throws and NEVER exits — enrichment is a bonus, not
 * a requirement, so a missing Solo must not break `list`.
 */
export const tryLoadToolNames = async (
  opts: { cwd?: string } = {},
): Promise<Map<number, string> | null> => {
  const cwd = opts.cwd ?? process.cwd();
  let client: SoloClient;
  try {
    const loaded = loadConfig({ cwd });
    const command = resolveTransportCommand(loaded.config.solo.transport.command);
    const transport = new StdioTransport({ ...loaded.config.solo.transport, command });
    client = new SoloClient(transport, { cwd, env: process.env });
    await client.connect();
  } catch {
    return null;
  }
  try {
    const tools = await client.listAgentTools();
    return new Map(tools.map((t) => [t.id, t.name] as const));
  } catch {
    return null;
  } finally {
    try {
      await client.disconnect();
    } catch {
      // ignore close errors — best-effort enrichment
    }
  }
};

export type PresetRemoveResult =
  | {
      readonly status: "removed";
      readonly preset: string;
      readonly definition: PresetDefinition;
      /** True when removing this definition emptied its preset, so the key was pruned. */
      readonly prunedPreset: boolean;
    }
  | { readonly status: "not_found"; readonly id: string };

/**
 * Core of `duo config preset remove`: read config OFFLINE (D7), locate the single
 * definition carrying the stable `id` (D2) across ALL presets, remove exactly it,
 * prune the preset key if it becomes empty, and persist via the Task-2 writer.
 *
 * On `not_found` it writes **nothing** and returns the outcome so the caller can
 * print an error and exit non-zero. Because `id` is globally unique (D2), the first
 * match is the only match — survivors are never reindexed. Reads/writes config via
 * the writer, which honors `resolveConfigPath()` (test with `DUO_CONFIG`).
 */
export const presetRemove = (id: string): PresetRemoveResult => {
  const raw = readRawConfig();
  const presets =
    raw.presets && typeof raw.presets === "object" && !Array.isArray(raw.presets)
      ? (raw.presets as Record<string, PresetDefinition[]>)
      : {};

  for (const [name, defs] of Object.entries(presets)) {
    if (!Array.isArray(defs)) continue;
    const idx = defs.findIndex(
      (def) => def && typeof def === "object" && (def as { id?: unknown }).id === id,
    );
    if (idx === -1) continue;

    const [removed] = defs.splice(idx, 1);
    let prunedPreset = false;
    if (defs.length === 0) {
      delete presets[name];
      prunedPreset = true;
    }
    raw.presets = presets;
    writeConfig(raw);

    return { status: "removed", preset: name, definition: removed!, prunedPreset };
  }

  return { status: "not_found", id };
};

const removeCommand = defineCommand({
  meta: {
    name: "remove",
    description: "Remove a preset definition by its stable id (offline)",
  },
  args: {
    id: { type: "positional", required: true, description: "Definition id to remove" },
    cwd: { type: "string", description: "Working directory" },
    json: { type: "boolean", description: "Emit JSON describing what was removed" },
    quiet: { type: "boolean", alias: "q", description: "Print only the removed definition id" },
  },
  async run({ args }) {
    const id = String(args.id ?? "").trim();
    if (!id) {
      writeErr("A definition id is required, e.g. `duo config preset remove aaaa1111`.");
      process.exit(EXIT_USER_ERROR);
    }

    let result: PresetRemoveResult;
    try {
      result = presetRemove(id);
    } catch (err) {
      writeErr(err instanceof Error ? err.message : String(err));
      process.exit(EXIT_USER_ERROR);
      return;
    }

    if (result.status === "not_found") {
      writeErr(`No preset definition with id "${result.id}" exists. Nothing was removed.`);
      process.exit(EXIT_USER_ERROR);
    }

    const { preset, definition, prunedPreset } = result;
    if (args.json) {
      writeJson({ removed: definition, preset, pruned_preset: prunedPreset });
      return;
    }
    if (args.quiet) {
      writeOut(definition.id);
      return;
    }
    writeOut(`removed:     ${definition.id}`);
    writeOut(`preset:      ${preset}${prunedPreset ? " (now empty — key pruned)" : ""}`);
    writeOut(`tool:        #${definition.agent_tool_id}`);
  },
});

const PRESET_LIST_COLUMNS: readonly Column<PresetRow>[] = [
  { header: "PRESET", get: (r) => r.preset },
  { header: "ID", get: (r) => r.id },
  { header: "TOOL", get: (r) => r.tool, truncate: 40 },
  { header: "EXTRA_ARGS", get: (r) => r.extra_args, truncate: 40 },
  { header: "PROVIDER", get: (r) => r.provider },
];

const listCommand = defineCommand({
  meta: {
    name: "list",
    description: "List presets and their definitions (offline; best-effort tool names)",
  },
  args: {
    name: { type: "positional", required: false, description: "Filter to a single preset" },
    cwd: { type: "string", description: "Working directory" },
    json: { type: "boolean", description: "Emit the raw structured view (presets → definitions)" },
    quiet: { type: "boolean", alias: "q", description: "Print only definition ids, one per line" },
  },
  async run({ args }) {
    const filter = args.name !== undefined ? String(args.name).trim() : undefined;

    let presets: Presets;
    try {
      presets = readPresets();
    } catch (err) {
      writeErr(err instanceof Error ? err.message : String(err));
      process.exit(EXIT_USER_ERROR);
    }

    // Only the human table needs live names; --json is raw and --quiet is ids,
    // so skip the (subprocess-spawning) Solo enrichment for those paths.
    const toolNames =
      args.json || args.quiet ? null : await tryLoadToolNames({ cwd: args.cwd });

    const view = buildPresetView(presets, { filter, toolNames });

    if (args.json) {
      writeJson(view.presets);
      return;
    }
    if (args.quiet) {
      for (const r of view.rows) writeOut(r.id);
      return;
    }
    if (view.rows.length === 0) {
      writeOut(
        view.filterMissing
          ? `No preset named "${filter}" is configured.`
          : "No presets configured.",
      );
      return;
    }
    writeOut(renderTable(view.rows, PRESET_LIST_COLUMNS));
  },
});

/**
 * The `preset` subcommand group under `duo config`. Task-3 ships `add`,
 * Task-4 `list`, Task-5 `remove`.
 */
export const presetCommand = defineCommand({
  meta: {
    name: "preset",
    description: "Manage Duo presets (label → agent tool + extra args + provider)",
  },
  subCommands: {
    add: addCommand,
    list: listCommand,
    remove: removeCommand,
  },
});
