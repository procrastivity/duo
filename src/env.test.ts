import { describe, expect, it } from "vitest";

import { detectSoloEnv } from "./env";

describe("detectSoloEnv", () => {
  it("returns values when env vars are set", () => {
    const result = detectSoloEnv({
      SOLO_PROCESS_ID: "process-42",
      SOLO_PROJECT_ID: "project-99",
    });

    expect(result).toEqual({
      soloProcessId: "process-42",
      soloProjectId: "project-99",
    });
  });

  it("returns undefined values when env vars are absent", () => {
    const result = detectSoloEnv({});

    expect(result).toEqual({
      soloProcessId: undefined,
      soloProjectId: undefined,
    });
  });

  it("has no side effects on the provided env object", () => {
    const env = {
      SOLO_PROCESS_ID: "process-42",
      SOLO_PROJECT_ID: "project-99",
    };

    const before = { ...env };
    detectSoloEnv(env);

    expect(env).toEqual(before);
  });
});
