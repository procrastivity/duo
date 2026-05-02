import { z } from "zod";
import { PolicySchema, type Policy } from "./types/policy.js";

export const soloStdioTransportSchema = z
  .object({
    type: z.literal("stdio"),
    command: z.string().min(1, "solo.transport.command is required"),
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
        processId: z.string().min(1, "solo.processId cannot be empty").optional(),
        projectId: z.string().min(1, "solo.projectId cannot be empty").optional(),
      })
      .strict(),
    policy: PolicySchema.optional(),
  })
  .strict();

export type SoloConfig = z.infer<typeof soloConfigSchema>;

type EnvSource = Record<string, string | undefined>;

export const detectSoloEnv = (env: EnvSource = process.env): {
  processId?: string;
  projectId?: string;
} => ({
  processId: env.SOLO_PROCESS_ID,
  projectId: env.SOLO_PROJECT_ID,
});

const formatZodError = (error: z.ZodError): string => {
  const first = error.issues[0];

  if (!first) {
    return "Invalid config";
  }

  const path = first.path.join(".") || "config";
  return `${path}: ${first.message}`;
};

export const parseConfig = (
  input: unknown,
  env: EnvSource = process.env,
): SoloConfig => {
  const source = (input ?? {}) as Record<string, unknown>;
  const detected = detectSoloEnv(env);

  const merged = {
    ...source,
    solo: {
      ...(source.solo as Record<string, unknown> | undefined),
      processId:
        ((source.solo as Record<string, unknown> | undefined)?.processId as
          | string
          | undefined) ?? detected.processId,
      projectId:
        ((source.solo as Record<string, unknown> | undefined)?.projectId as
          | string
          | undefined) ?? detected.projectId,
    },
  };

  const result = soloConfigSchema.safeParse(merged);

  if (!result.success) {
    throw new Error(formatZodError(result.error));
  }

  return result.data;
};
