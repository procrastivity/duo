import { z } from "zod";
import { isProviderEnabled as defaultIsProviderEnabled } from "../state/providers.js";
import type { PresetDefinition, Presets } from "../types/presets.js";

export const ListPresetsInputSchema = z.object({}).strict();

export type ListPresetsInput = z.infer<typeof ListPresetsInputSchema>;

export interface PresetDefinitionView {
  id: string;
  agent_tool_id: number;
  provider?: string;
  /** Provider enabled-state at read time; always true for a no-provider def. */
  enabled: boolean;
}

export interface PresetAvailability {
  /**
   * True when the preset (or its `default` fallback) has at least one eligible
   * definition — mirrors the D4 no-`avoid` ladder without the random pick.
   */
  available: boolean;
  definitions: PresetDefinitionView[];
}

export type ListPresetsResult = Record<string, PresetAvailability>;

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

const DEFAULT_PRESET = "default";

const viewDefs = (
  defs: readonly PresetDefinition[],
  isEnabled: (provider: string) => boolean,
): PresetDefinitionView[] =>
  defs.map((d) => ({
    id: d.id,
    agent_tool_id: d.agent_tool_id,
    ...(d.provider !== undefined ? { provider: d.provider } : {}),
    enabled: d.provider === undefined || isEnabled(d.provider),
  }));

/**
 * Enumerate the configured presets and report per-preset availability. Purely
 * config- + provider-state driven — it does not consult Solo. `isProviderEnabled`
 * defaults to the filesystem-backed reader and is injectable for tests.
 */
export function listPresets(
  presets: Presets | undefined,
  options: { isProviderEnabled?: (provider: string) => boolean } = {},
): ListPresetsResult {
  const isEnabled = options.isProviderEnabled ?? defaultIsProviderEnabled;
  const map = presets ?? {};

  const defaultDefs = map[DEFAULT_PRESET] ?? [];
  const defaultHasEligible = viewDefs(defaultDefs, isEnabled).some(
    (d) => d.enabled,
  );

  const result: ListPresetsResult = {};
  for (const [name, defs] of Object.entries(map)) {
    const definitions = viewDefs(defs, isEnabled);
    const selfEligible = definitions.some((d) => d.enabled);
    result[name] = {
      available:
        selfEligible || (name !== DEFAULT_PRESET && defaultHasEligible),
      definitions,
    };
  }
  return result;
}

/**
 * MCP handler for `list_presets`: wraps {@link listPresets} in the MCP content
 * envelope so the result is delivered as text content (like the other tools),
 * rather than a bare object that renders as empty output.
 */
export function listPresetsHandler(presets: Presets | undefined): ToolResult {
  return ok(listPresets(presets));
}
