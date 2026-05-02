import { z } from "zod";

export const SoloAgentToolSchema = z.object({
  id: z.number(),
  name: z.string(),
  command: z.string(),
  tool_type: z.string(),
  enabled: z.boolean(),
});

export type SoloAgentTool = z.infer<typeof SoloAgentToolSchema>;

export const SoloAgentToolsSchema = z.array(SoloAgentToolSchema);
