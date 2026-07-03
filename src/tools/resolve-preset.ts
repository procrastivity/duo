import { z } from "zod";
import { resolvePreset, type ResolvePresetOptions } from "../resolver.js";
import { PresetUnavailableError, UnknownPresetError } from "../errors.js";
import type { Logger } from "../logger.js";
import type { Presets } from "../types/presets.js";

export const ResolvePresetInputSchema = z
  .object({
    preset: z.string().min(1, "preset is required"),
    avoid_provider: z.string().optional(),
  })
  .strict();

export type ResolvePresetInput = z.infer<typeof ResolvePresetInputSchema>;

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
 * (`rng`, injected `isProviderEnabled`); production callers pass nothing. The
 * public `avoid_provider` input is threaded into `options.avoidProvider` (D5 —
 * pure input plumbing; the resolver capability already exists).
 */
export async function resolvePresetHandler(
  logger: Logger,
  input: ResolvePresetInput,
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
  try {
    const resolution = resolvePreset(presets ?? {}, presetName, resolveOptions);
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
