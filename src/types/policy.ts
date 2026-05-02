import { z } from "zod";

const TokenListSchema = z.array(z.string().min(1)).default([]);

const TierTokenOverrideSchema = z
  .object({
    // "extend" (default): merge override tokens with built-in tokens, dedup case-insensitively.
    // "replace": override tokens become the entire token set for this tier; built-ins discarded.
    mode: z.enum(["extend", "replace"]).default("extend"),
    tokens: TokenListSchema,
  })
  .strict();

const CommandTokenOverridesSchema = z
  .object({
    small: TierTokenOverrideSchema.optional(),
    medium: TierTokenOverrideSchema.optional(),
    large: TierTokenOverrideSchema.optional(),
  })
  .strict();

const PreferenceSelectorSchema = z
  .object({
    // At least one of tool_type / tool_name must be present (refine).
    tool_type: z.string().min(1).optional(),
    tool_name: z.string().min(1).optional(),
  })
  .strict()
  .refine(
    (s) => s.tool_type !== undefined || s.tool_name !== undefined,
    { message: "selector must specify tool_type and/or tool_name" },
  );

const SelectionPolicySchema = z
  .object({
    // When `preference` is present, strategy becomes "custom"; otherwise it remains "random".
    // First selector that matches a candidate wins; ties within the matched bucket fall back
    // to the existing random RNG. Unmatched candidates remain eligible at the end of the list
    // and are also resolved via random tiebreak. Order matters.
    preference: z.array(PreferenceSelectorSchema).min(1).optional(),
  })
  .strict();

export const PolicySchema = z
  .object({
    command_tokens: CommandTokenOverridesSchema.optional(),
    selection: SelectionPolicySchema.optional(),
  })
  .strict();

export type Policy = z.infer<typeof PolicySchema>;
export type PreferenceSelector = z.infer<typeof PreferenceSelectorSchema>;
