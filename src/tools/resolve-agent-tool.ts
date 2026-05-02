import { z } from "zod";
import { SoloClient, SoloClientError } from "../solo-client.js";
import { resolveAgentTool } from "../resolver.js";
import { TierUnavailableError, UnsupportedTierError, TIER_LABELS } from "../errors.js";
import type { Logger } from "../logger.js";
import type { ClassifierTokenPolicy } from "../classifier.js";
import type { PreferenceSelector } from "../types/policy.js";

export const ResolveAgentToolInputSchema = z
  .object({
    tier: z.string().min(1, "tier is required"),
  })
  .strict();

export type ResolveAgentToolInput = z.infer<typeof ResolveAgentToolInputSchema>;

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
  content: [{ type: "text", text: JSON.stringify({ code, message, ...extra }) }],
  isError: true,
});

export async function resolveAgentToolHandler(
  soloClient: SoloClient,
  logger: Logger,
  input: ResolveAgentToolInput,
  classifierPolicy?: ClassifierTokenPolicy,
  preference?: PreferenceSelector[],
): Promise<ToolResult> {
  let tools;
  try {
    tools = await soloClient.listAgentTools();
  } catch (err) {
    if (err instanceof SoloClientError) {
      logger.resolutionFailure({
        requested_tier: input.tier,
        error_code: String(err.code),
        available_tiers: TIER_LABELS,
      });
      return mcpError(err.code, err.message);
    }
    throw err;
  }

  try {
    const options = {
      classifierPolicy,
      ...(preference && { preference, strategy: "custom" as const }),
    };
    const resolution = resolveAgentTool(tools, input.tier, options);
    logger.resolutionSuccess({
      requested_tier: input.tier as "small" | "medium" | "large",
      selected_tool_id: resolution.selected.agent_tool_id,
      selected_tool_name: resolution.selected.tool_name,
      match_source: resolution.classification_source,
      candidate_count: resolution.diagnostics.candidates_considered,
      token_source: resolution.selected.token_source,
      strategy: resolution.diagnostics.strategy,
      preference_applied: resolution.diagnostics.preference_applied,
    });
    return ok(resolution);
  } catch (err) {
    if (err instanceof UnsupportedTierError) {
      logger.resolutionFailure({
        requested_tier: input.tier,
        error_code: "unsupported_tier",
        available_tiers: TIER_LABELS,
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
        available_tiers: TIER_LABELS,
      });
      return mcpError(
        err.code,
        `No enabled candidates available for tier "${err.diagnostics.requested_tier}".`,
        { diagnostics: err.diagnostics },
      );
    }
    throw err;
  }
}
