import { z } from "zod";
import { SoloClient, SoloClientError } from "../solo-client.js";
import { resolvePreset, type ResolvePresetOptions } from "../resolver.js";
import { PresetUnavailableError, UnknownPresetError } from "../errors.js";
import type { Logger } from "../logger.js";
import type { Presets } from "../types/presets.js";

export const LaunchAgentInputSchema = z
  .object({
    preset: z.string().min(1, "preset is required"),
    name: z.string().min(1).optional(),
    project_id: z.number().int().nonnegative().optional(),
    avoid_provider: z.string().optional(),
    // Caller-supplied append; string[] (OQ1) matches SoloSpawnArgsSchema.extra_args
    // and the resolver's tokenized output — a plain concat, no tokenizer here.
    extra_args: z.array(z.string()).optional(),
  })
  .strict();

export type LaunchAgentInput = z.infer<typeof LaunchAgentInputSchema>;

export interface LaunchAgentResult {
  process_id: number;
  name: string;
  preset: string;
  agent_tool_id: number;
  extra_args: string[];
  // Always present (D4/OQ2): the provider label when the selected definition
  // has one, `null` when it has none (chain reads `null` as "nothing to avoid").
  provider: string | null;
  project_id?: number;
}

interface TextContent {
  type: "text";
  text: string;
}

interface ToolResult {
  content: TextContent[];
  isError?: boolean;
}

const ok = (data: unknown): ToolResult => ({
  content: [{ type: "text", text: JSON.stringify(data) }],
});

const mcpError = (
  code: string | number,
  message: string,
  extra?: Record<string, unknown>,
): ToolResult => ({
  content: [
    { type: "text", text: JSON.stringify({ code, message, ...extra }) },
  ],
  isError: true,
});

/**
 * Resolve a preset and launch the selected agent tool as a Solo process. The
 * resolver needs only the config `presets` + provider enabled-state (no Solo
 * agent-tool list); the Solo client is still required to spawn. The resolved
 * `extra_args` (tokenized) are threaded into the spawn call, with any caller
 * `extra_args` appended after them (D3 order: resolved first, caller second) —
 * this is where the preset engine (deliverable a) meets the transport plumbing
 * (deliverable c). `input.avoid_provider` threads into `options.avoidProvider`
 * (D5 — pure input plumbing; the resolver capability already exists).
 * `options` carries the resolver test seams; production callers pass nothing.
 */
export async function launchAgentHandler(
  soloClient: SoloClient,
  logger: Logger,
  input: LaunchAgentInput,
  presets: Presets | undefined,
  options: ResolvePresetOptions = {},
): Promise<ToolResult> {
  const presetName = input.preset;
  const resolveOptions: ResolvePresetOptions = {
    ...options,
    ...(input.avoid_provider !== undefined
      ? { avoidProvider: input.avoid_provider }
      : {}),
  };

  let resolution;
  try {
    resolution = resolvePreset(presets ?? {}, presetName, resolveOptions);
  } catch (err) {
    if (err instanceof UnknownPresetError) {
      logger.resolutionFailure({
        requested_preset: presetName,
        error_code: err.code,
      });
      return mcpError(err.code, err.message, { preset: err.preset });
    }
    if (err instanceof PresetUnavailableError) {
      logger.resolutionFailure({
        requested_preset: presetName,
        error_code: err.code,
      });
      return mcpError(err.code, err.message, { diagnostics: err.diagnostics });
    }
    throw err;
  }

  logger.resolutionSuccess({
    requested_preset: resolution.preset_requested,
    preset_used: resolution.preset_used,
    selected_tool_id: resolution.agent_tool_id,
    fell_back_to_default: resolution.fell_back_to_default,
    relented_on_avoid_provider: resolution.relented_on_avoid_provider,
  });

  // D3: resolved preset args first, caller append second. The merged effective
  // array is what reaches Solo and what result.extra_args reports.
  const mergedExtraArgs = [
    ...resolution.extra_args,
    ...(input.extra_args ?? []),
  ];

  const spawnArgs: {
    kind: "agent";
    agent_tool_id: number;
    name?: string;
    project_id?: number;
    extra_args?: string[];
  } = {
    kind: "agent",
    agent_tool_id: resolution.agent_tool_id,
  };
  if (input.name !== undefined) spawnArgs.name = input.name;
  if (input.project_id !== undefined) spawnArgs.project_id = input.project_id;
  if (mergedExtraArgs.length > 0) spawnArgs.extra_args = mergedExtraArgs;

  let spawnResult;
  try {
    spawnResult = await soloClient.spawnProcess(spawnArgs);
  } catch (err) {
    if (err instanceof SoloClientError) {
      const data: Record<string, unknown> = {
        solo_code: err.code,
        requested_preset: presetName,
        agent_tool_id: resolution.agent_tool_id,
      };
      if (input.name !== undefined) data.requested_name = input.name;
      if (input.project_id !== undefined)
        data.requested_project_id = input.project_id;
      return mcpError("spawn_rejected", err.message, { data });
    }
    throw err;
  }

  logger.spawnSuccess({
    requested_preset: presetName,
    selected_tool_id: resolution.agent_tool_id,
    solo_process_id: String(spawnResult.process_id),
    process_name: spawnResult.name,
  });

  const effectiveProjectId = input.project_id ?? soloClient.projectId;

  const result: LaunchAgentResult = {
    process_id: spawnResult.process_id,
    name: spawnResult.name,
    preset: presetName,
    agent_tool_id: resolution.agent_tool_id,
    extra_args: mergedExtraArgs,
    provider: resolution.provider ?? null,
  };
  if (effectiveProjectId !== undefined) result.project_id = effectiveProjectId;

  return ok(result);
}
