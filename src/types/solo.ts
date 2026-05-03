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

export const SoloProjectSchema = z.object({
  id: z.number(),
  name: z.string(),
  path: z.string(),
});

export type SoloProject = z.infer<typeof SoloProjectSchema>;

export const SoloProjectsSchema = z.array(SoloProjectSchema);

export const SoloSpawnArgsSchema = z.object({
  kind: z.literal("agent"),
  agent_tool_id: z.number(),
  name: z.string().optional(),
  project_id: z.number().int().optional(),
});

export type SoloSpawnArgs = z.infer<typeof SoloSpawnArgsSchema>;

export const SoloSpawnResultSchema = z
  .object({
    process_id: z.number(),
    name: z.string(),
  })
  .passthrough();

export type SoloSpawnResult = z.infer<typeof SoloSpawnResultSchema>;
