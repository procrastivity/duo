import { z } from "zod";
import { resolvePreset, type ResolvePresetOptions } from "../resolver.js";
import { PresetUnavailableError, UnknownPresetError } from "../errors.js";
import type { Logger } from "../logger.js";
import type { Presets } from "../types/presets.js";

export const ResolveAgentToolInputSchema = z
  .object({
    // Public wire key kept as `tier` for step-03 (OQ1); it names a preset now.
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

/**
 * Resolve which agent tool a preset selects. The resolver is provider-aware and
 * needs only the config `presets` plus per-provider enabled-state — it no longer
 * consults the Solo agent-tool list. `options` carries the resolver test seams
 * (`rng`, injected `isProviderEnabled`); production callers pass nothing.
 */
export async function resolveAgentToolHandler(
  logger: Logger,
  input: ResolveAgentToolInput,
  presets: Presets | undefined,
  options: ResolvePresetOptions = {},
): Promise<ToolResult> {
  const presetName = input.tier;
  try {
    const resolution = resolvePreset(presets ?? {}, presetName, options);
    logger.resolutionSuccess({
      requested_preset: resolution.preset_requested,
      preset_used: resolution.preset_used,
      selected_tool_id: resolution.agent_tool_id,
      fell_back_to_default: resolution.fell_back_to_default,
      relented_on_avoid_provider: resolution.relented_on_avoid_provider,
    });
    return ok(resolution);
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
}
