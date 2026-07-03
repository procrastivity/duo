import { z } from "zod";
import { isValidProviderLabel } from "../state/paths.js";

const ProviderLabelSchema = z
  .string()
  .min(1)
  .refine(isValidProviderLabel, {
    message:
      'Provider labels must match ^[A-Za-z0-9._-]+$ and cannot be "", ".", "..", or contain a path separator.',
  });

export const PresetDefinitionSchema = z
  .object({
    id: z.string().min(1),
    agent_tool_id: z.number().int(),
    extra_args: z.string().optional(),
    provider: ProviderLabelSchema.optional(),
  })
  .strict();

export type PresetDefinition = z.infer<typeof PresetDefinitionSchema>;

export const PresetsSchema = z.record(
  z.string().min(1),
  z.array(PresetDefinitionSchema),
);

export type Presets = z.infer<typeof PresetsSchema>;
