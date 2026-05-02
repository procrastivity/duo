import { z } from "zod";
import { McpError } from "@modelcontextprotocol/sdk/types.js";
import { TierUnavailableError, type ResolverDiagnostics } from "../errors.js";
import { resolveAgentTool } from "../resolver.js";
import { SoloClientError, type SoloClient } from "../solo-client.js";
import type { SoloAgentTool } from "../types/solo.js";

export const ListAgentTiersInputSchema = z.object({}).strict();

export type ListAgentTiersInput = z.infer<typeof ListAgentTiersInputSchema>;

export interface TierAvailabilityDefault {
  agent_tool_id: number;
  tool_name: string;
  tool_type: string;
  command: string;
  classification_source: "command" | "name_fallback";
}

export interface TierAvailabilityAlternative {
  agent_tool_id: number;
  tool_name: string;
  tool_type: string;
  classification_source: "command" | "name_fallback";
}

export interface TierAvailability {
  available: boolean;
  default?: TierAvailabilityDefault;
  alternatives: TierAvailabilityAlternative[];
  diagnostics: ResolverDiagnostics;
}

export interface ListAgentTiersResult {
  small: TierAvailability;
  medium: TierAvailability;
  large: TierAvailability;
}

const TIERS = ["small", "medium", "large"] as const;

function resolveTier(
  tools: readonly SoloAgentTool[],
  tier: "small" | "medium" | "large",
): TierAvailability {
  try {
    const resolution = resolveAgentTool(tools, tier);
    return {
      available: true,
      default: {
        agent_tool_id: resolution.selected.agent_tool_id,
        tool_name: resolution.selected.tool_name,
        tool_type: resolution.selected.tool_type,
        command: resolution.selected.command,
        classification_source: resolution.classification_source,
      },
      alternatives: resolution.alternatives,
      diagnostics: resolution.diagnostics,
    };
  } catch (err) {
    if (err instanceof TierUnavailableError) {
      return {
        available: false,
        alternatives: [],
        diagnostics: err.diagnostics,
      };
    }
    throw err;
  }
}

export async function listAgentTiers(
  client: SoloClient,
): Promise<ListAgentTiersResult> {
  let tools: SoloAgentTool[];
  try {
    tools = await client.listAgentTools();
  } catch (err) {
    if (err instanceof SoloClientError) {
      throw new McpError(err.code, err.message);
    }
    throw err;
  }

  return {
    small: resolveTier(tools, "small"),
    medium: resolveTier(tools, "medium"),
    large: resolveTier(tools, "large"),
  };
}
