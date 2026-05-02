import { z } from "zod";
import { SoloClient, SoloClientError } from "../solo-client.js";
import { resolveAgentTool } from "../resolver.js";
import { TierUnavailableError, UnsupportedTierError, TIER_LABELS } from "../errors.js";

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
  input: ResolveAgentToolInput,
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

  try {
    const resolution = resolveAgentTool(tools, input.tier);
    return ok(resolution);
  } catch (err) {
    if (err instanceof UnsupportedTierError) {
      return mcpError(
        err.code,
        `Unsupported tier "${err.requested}". Supported tiers: ${TIER_LABELS.join(", ")}.`,
      );
    }
    if (err instanceof TierUnavailableError) {
      return mcpError(
        err.code,
        `No enabled candidates available for tier "${err.diagnostics.requested_tier}".`,
        { diagnostics: err.diagnostics },
      );
    }
    throw err;
  }
}
