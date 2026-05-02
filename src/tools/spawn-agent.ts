import { z } from "zod";
import { SoloClient, SoloClientError } from "../solo-client.js";
import { resolveAgentTool } from "../resolver.js";
import {
  TierUnavailableError,
  UnsupportedTierError,
  TIER_LABELS,
} from "../errors.js";
import type { SoloConfig } from "../config.js";
import type { Logger } from "../logger.js";
import type { ClassifierTokenPolicy } from "../classifier.js";
import type { PreferenceSelector } from "../types/policy.js";

export const SpawnAgentInputSchema = z
  .object({
    tier: z.string().min(1, "tier is required"),
    name: z.string().min(1).optional(),
    project_id: z.string().min(1).optional(),
  })
  .strict();

export type SpawnAgentInput = z.infer<typeof SpawnAgentInputSchema>;

export interface SpawnAgentToolSummary {
  agent_tool_id: number;
  tool_name: string;
  tool_type: string;
  command: string;
  classification_source: "command" | "name_fallback";
}

export interface SpawnAgentResult {
  process_id: string;
  name: string;
  tier: "small" | "medium" | "large";
  tool: SpawnAgentToolSummary;
  project_id?: string;
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

export const resolveProjectId = (
  input: { project_id?: string },
  config: SoloConfig,
): string | undefined => {
  if (input.project_id !== undefined) return input.project_id;
  if (config.solo.projectId !== undefined) return config.solo.projectId;
  return undefined;
};

export async function spawnAgentHandler(
  soloClient: SoloClient,
  config: SoloConfig,
  logger: Logger,
  input: SpawnAgentInput,
  classifierPolicy?: ClassifierTokenPolicy,
  preference?: PreferenceSelector[],
): Promise<ToolResult> {
  let tools;
  try {
    tools = await soloClient.listAgentTools();
  } catch (err) {
    if (err instanceof SoloClientError) {
      return mcpError(err.code, err.message);
    }
    throw err;
  }

  let resolution;
  try {
    const options = {
      classifierPolicy,
      ...(preference && { preference, strategy: "custom" as const }),
    };
    resolution = resolveAgentTool(tools, input.tier, options);
  } catch (err) {
    if (err instanceof UnsupportedTierError) {
      logger.resolutionFailure({
        requested_tier: input.tier,
        error_code: "unsupported_tier",
        available_tiers: TIER_LABELS as ("small" | "medium" | "large")[],
      });
      return mcpError(
        err.code,
        `Unsupported tier "${err.requested}". Supported tiers: ${TIER_LABELS.join(", ")}.`,
      );
    }
    if (err instanceof TierUnavailableError) {
      logger.resolutionFailure({
        requested_tier: input.tier,
        error_code: "tier_unavailable",
        available_tiers: TIER_LABELS as ("small" | "medium" | "large")[],
      });
      return mcpError(
        err.code,
        `No enabled candidates available for tier "${err.diagnostics.requested_tier}".`,
        { diagnostics: err.diagnostics },
      );
    }
    throw err;
  }

  // Log successful resolution (before spawn attempt)
  const tier = input.tier as "small" | "medium" | "large";
  logger.resolutionSuccess({
    requested_tier: tier,
    selected_tool_id: resolution.selected.agent_tool_id,
    selected_tool_name: resolution.selected.tool_name,
    match_source: resolution.classification_source,
    candidate_count: resolution.diagnostics.candidates_considered,
    token_source: resolution.selected.token_source,
    strategy: resolution.diagnostics.strategy,
    preference_applied: resolution.diagnostics.preference_applied,
  });

  const effectiveProjectId = resolveProjectId(input, config);

  const spawnArgs: {
    kind: "agent";
    agent_tool_id: number;
    name?: string;
    project_id?: string;
  } = {
    kind: "agent",
    agent_tool_id: resolution.selected.agent_tool_id,
  };
  if (input.name !== undefined) spawnArgs.name = input.name;
  if (effectiveProjectId !== undefined) spawnArgs.project_id = effectiveProjectId;

  let spawnResult;
  try {
    spawnResult = await soloClient.spawnProcess(spawnArgs);
  } catch (err) {
    if (err instanceof SoloClientError) {
      const data: Record<string, unknown> = {
        solo_code: err.code,
        requested_tier: input.tier,
        agent_tool_id: resolution.selected.agent_tool_id,
      };
      if (input.name !== undefined) data.requested_name = input.name;
      if (effectiveProjectId !== undefined)
        data.requested_project_id = effectiveProjectId;
      return mcpError("spawn_rejected", err.message, { data });
    }
    throw err;
  }

  // Log successful spawn
  logger.spawnSuccess({
    requested_tier: tier,
    selected_tool_id: resolution.selected.agent_tool_id,
    solo_process_id: spawnResult.process_id,
    process_name: spawnResult.name,
  });

  const result: SpawnAgentResult = {
    process_id: spawnResult.process_id,
    name: spawnResult.name,
    tier,
    tool: {
      agent_tool_id: resolution.selected.agent_tool_id,
      tool_name: resolution.selected.tool_name,
      tool_type: resolution.selected.tool_type,
      command: resolution.selected.command,
      classification_source: resolution.classification_source,
    },
  };
  if (effectiveProjectId !== undefined) result.project_id = effectiveProjectId;

  return ok(result);
}
