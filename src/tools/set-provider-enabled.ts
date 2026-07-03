import { z } from "zod";
import { setProviderEnabled } from "../state/providers.js";

export const SetProviderEnabledInputSchema = z
  .object({
    provider: z.string().min(1, "provider is required"),
    enabled: z.boolean(),
  })
  .strict();

export type SetProviderEnabledInput = z.infer<typeof SetProviderEnabledInputSchema>;

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
 * Toggle a provider's enabled-state in the XDG provider-state directory. Offline
 * (D6): writes only provider state via `setProviderEnabled()`, never consults
 * Solo. `setProviderEnabled` validates the label first (via
 * `assertValidProviderLabel`), which throws before any filesystem write on an
 * invalid label. That throw is caught here and converted to a structured
 * `invalid_provider_label` tool error so no raw throw escapes and no state is
 * written for a bad label.
 */
export function setProviderEnabledHandler(
  input: SetProviderEnabledInput,
): ToolResult {
  try {
    setProviderEnabled(input.provider, input.enabled);
  } catch (err) {
    return mcpError(
      "invalid_provider_label",
      err instanceof Error ? err.message : String(err),
      { provider: input.provider },
    );
  }
  return ok({ provider: input.provider, enabled: input.enabled });
}
