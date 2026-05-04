import { z } from "zod";
import { PolicySchema } from "./types/policy.js";

export const soloStdioTransportSchema = z
  .object({
    type: z.literal("stdio"),
    command: z.string().min(1, "solo.transport.command cannot be empty when set").optional(),
    args: z.array(z.string()).optional().default([]),
    cwd: z.string().min(1, "solo.transport.cwd cannot be empty").optional(),
    env: z.record(z.string(), z.string()).optional(),
  })
  .strict();

export const soloConfigSchema = z
  .object({
    solo: z
      .object({
        transport: soloStdioTransportSchema,
      })
      .strict(),
    policy: PolicySchema.optional(),
  })
  .strict();

export type SoloConfig = z.infer<typeof soloConfigSchema>;

const formatZodError = (error: z.ZodError): string => {
  const first = error.issues[0];

  if (!first) {
    return "Invalid config";
  }

  const path = first.path.join(".") || "config";
  return `${path}: ${first.message}`;
};

export const parseConfig = (input: unknown): SoloConfig => {
  const result = soloConfigSchema.safeParse(input ?? {});

  if (!result.success) {
    throw new Error(formatZodError(result.error));
  }

  return result.data;
};
