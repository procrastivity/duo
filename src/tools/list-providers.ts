import { z } from "zod";
import { listProviders } from "../state/providers.js";

export const ListProvidersInputSchema = z.object({}).strict();

export type ListProvidersInput = z.infer<typeof ListProvidersInputSchema>;

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

/**
 * List the providers tracked in the XDG provider-state directory with their
 * enabled-state. Offline (D6): reads only provider state via `listProviders()`,
 * never consults Solo. Scope is state-dir only — providers discovered from preset
 * definitions are intentionally NOT unioned in here (OQ3; deferred follow-up).
 */
export function listProvidersHandler(): ToolResult {
  return ok({ providers: listProviders() });
}
