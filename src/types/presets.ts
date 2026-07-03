import { z } from "zod";

export const PresetDefinitionSchema = z
  .object({
    id: z.string().min(1),
    agent_tool_id: z.number().int(),
    extra_args: z.string().optional(),
    provider: z.string().min(1).optional(),
  })
  .strict();

export type PresetDefinition = z.infer<typeof PresetDefinitionSchema>;

export const PresetsSchema = z.record(
  z.string().min(1),
  z.array(PresetDefinitionSchema),
);

export type Presets = z.infer<typeof PresetsSchema>;
