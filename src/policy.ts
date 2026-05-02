import { z } from "zod";
import { Policy, PolicySchema } from "./types/policy";

const formatZodError = (error: z.ZodError): string => {
  const first = error.issues[0];

  if (!first) {
    return "Invalid policy";
  }

  const path = first.path.join(".") || "policy";
  return `${path}: ${first.message}`;
};

export const loadPolicy = (source: unknown): Policy => {
  const result = PolicySchema.safeParse(source ?? {});

  if (!result.success) {
    throw new Error(formatZodError(result.error));
  }

  return result.data;
};
