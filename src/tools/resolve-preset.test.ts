import { describe, expect, it } from "vitest";
import { resolvePresetHandler } from "./resolve-preset.js";
import type { Logger } from "../logger.js";
import type { Presets } from "../types/presets.js";

const parse = (result: { content: Array<{ text: string }> }) =>
  JSON.parse(result.content[0].text);

/** Fake Logger that records all calls for assertion in tests. */
const makeFakeLogger = () => {
  const calls: Array<{ method: string; fields: unknown }> = [];
  const logger: Logger = {
    resolutionSuccess(fields) {
      calls.push({ method: "resolutionSuccess", fields });
    },
    resolutionFailure(fields) {
      calls.push({ method: "resolutionFailure", fields });
    },
    spawnSuccess(fields) {
      calls.push({ method: "spawnSuccess", fields });
    },
  };
  return { logger, calls };
};

// Deterministic rng: floor(v * n) selects the pool index.
const seededRng =
  (v: number): (() => number) =>
  () =>
    v;

const allEnabled = (): boolean => true;
const disabledSet =
  (...off: string[]) =>
  (provider: string): boolean =>
    !off.includes(provider);

const presets: Presets = {
  builder: [
    { id: "b-anthropic", agent_tool_id: 1, provider: "anthropic" },
    { id: "b-openai", agent_tool_id: 2, provider: "openai" },
  ],
  default: [{ id: "d-anthropic", agent_tool_id: 10, provider: "anthropic" }],
};

describe("resolvePresetHandler", () => {
  describe("happy path — enabled providers", () => {
    it("returns the pool member indicated by rng", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolvePresetHandler(
        logger,
        { preset: "builder" },
        presets,
        { isProviderEnabled: allEnabled, rng: seededRng(0) },
      );
      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.agent_tool_id).toBe(1);
      expect(data.provider).toBe("anthropic");
      expect(data.preset_used).toBe("builder");
      expect(data.fell_back_to_default).toBe(false);
    });

    it("reaches every eligible definition across rng values", async () => {
      const seen = new Set<number>();
      for (const v of [0, 0.6]) {
        const { logger } = makeFakeLogger();
        const result = await resolvePresetHandler(
          logger,
          { preset: "builder" },
          presets,
          { isProviderEnabled: allEnabled, rng: seededRng(v) },
        );
        seen.add(parse(result).agent_tool_id);
      }
      expect(seen).toEqual(new Set([1, 2]));
    });
  });

  describe("avoid_provider plumbing", () => {
    it("steers to a different provider when avoid_provider is enabled elsewhere", async () => {
      // avoid anthropic → the not-avoided, enabled openai def is chosen; no relent.
      const { logger } = makeFakeLogger();
      const result = await resolvePresetHandler(
        logger,
        { preset: "builder", avoid_provider: "anthropic" },
        presets,
        { isProviderEnabled: allEnabled, rng: seededRng(0) },
      );
      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.agent_tool_id).toBe(2);
      expect(data.provider).toBe("openai");
      expect(data.relented_on_avoid_provider).toBe(false);
      expect(data.fell_back_to_default).toBe(false);
    });

    it("relents on avoid_provider when it is the only eligible def", async () => {
      // Only openai is available; avoiding it would leave nothing, so the
      // resolver softly relents rather than hard-failing (D4 ladder).
      const p: Presets = {
        builder: [{ id: "b-openai", agent_tool_id: 2, provider: "openai" }],
      };
      const { logger } = makeFakeLogger();
      const result = await resolvePresetHandler(
        logger,
        { preset: "builder", avoid_provider: "openai" },
        p,
        { isProviderEnabled: allEnabled, rng: seededRng(0) },
      );
      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.agent_tool_id).toBe(2);
      expect(data.provider).toBe("openai");
      expect(data.relented_on_avoid_provider).toBe(true);
    });

    it("logs the relented_on_avoid_provider diagnostic through the tool result", async () => {
      const p: Presets = {
        builder: [{ id: "b-openai", agent_tool_id: 2, provider: "openai" }],
      };
      const { logger, calls } = makeFakeLogger();
      await resolvePresetHandler(
        logger,
        { preset: "builder", avoid_provider: "openai" },
        p,
        { isProviderEnabled: allEnabled, rng: seededRng(0) },
      );
      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("relented_on_avoid_provider", true);
    });
  });

  describe("unknown preset", () => {
    it("returns isError with code unknown_preset and names the preset", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolvePresetHandler(
        logger,
        { preset: "nope" },
        presets,
        { isProviderEnabled: allEnabled },
      );
      expect(result.isError).toBe(true);
      const data = parse(result);
      expect(data.code).toBe("unknown_preset");
      expect(data.preset).toBe("nope");
    });

    it("returns unknown_preset when presets is undefined", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolvePresetHandler(
        logger,
        { preset: "builder" },
        undefined,
      );
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("unknown_preset");
    });
  });

  describe("default fallback", () => {
    it("falls back to default when the requested preset has no eligible def", async () => {
      // builder offers only openai (disabled); default (anthropic) stays enabled.
      const p: Presets = {
        builder: [{ id: "b-openai", agent_tool_id: 2, provider: "openai" }],
        default: [{ id: "d-anthropic", agent_tool_id: 10, provider: "anthropic" }],
      };
      const { logger } = makeFakeLogger();
      const result = await resolvePresetHandler(
        logger,
        { preset: "builder" },
        p,
        { isProviderEnabled: disabledSet("openai"), rng: seededRng(0) },
      );
      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.agent_tool_id).toBe(10);
      expect(data.preset_used).toBe("default");
      expect(data.fell_back_to_default).toBe(true);
    });
  });

  describe("preset unavailable", () => {
    const noDefault: Presets = {
      builder: [{ id: "b-openai", agent_tool_id: 2, provider: "openai" }],
    };

    it("returns isError with code preset_unavailable and diagnostics", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolvePresetHandler(
        logger,
        { preset: "builder" },
        noDefault,
        { isProviderEnabled: disabledSet("openai") },
      );
      expect(result.isError).toBe(true);
      const data = parse(result);
      expect(data.code).toBe("preset_unavailable");
      expect(data.diagnostics.requested_preset).toBe("builder");
      expect(data.diagnostics.disabled_providers).toEqual(["openai"]);
    });
  });

  describe("extra_args tokenization", () => {
    it("returns the tokenized extra_args on the resolution", async () => {
      const p: Presets = {
        solo: [
          { id: "s", agent_tool_id: 1, extra_args: "--model sonnet --flag" },
        ],
      };
      const { logger } = makeFakeLogger();
      const result = await resolvePresetHandler(
        logger,
        { preset: "solo" },
        p,
        { isProviderEnabled: allEnabled },
      );
      expect(parse(result).extra_args).toEqual(["--model", "sonnet", "--flag"]);
    });
  });

  describe("Logger instrumentation", () => {
    it("happy path — one resolutionSuccess call with preset fields", async () => {
      const { logger, calls } = makeFakeLogger();
      await resolvePresetHandler(logger, { preset: "builder" }, presets, {
        isProviderEnabled: allEnabled,
        rng: seededRng(0),
      });
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionSuccess");
      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_preset", "builder");
      expect(fields).toHaveProperty("preset_used", "builder");
      expect(fields).toHaveProperty("selected_tool_id", 1);
      expect(fields).toHaveProperty("fell_back_to_default", false);
      expect(fields).toHaveProperty("relented_on_avoid_provider", false);
      expect(fields).not.toHaveProperty("requested_name");
      expect(fields).not.toHaveProperty("prompt");
    });

    it("unknown preset — one resolutionFailure with error_code unknown_preset", async () => {
      const { logger, calls } = makeFakeLogger();
      await resolvePresetHandler(logger, { preset: "nope" }, presets, {
        isProviderEnabled: allEnabled,
      });
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionFailure");
      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_preset", "nope");
      expect(fields).toHaveProperty("error_code", "unknown_preset");
    });

    it("preset unavailable — one resolutionFailure with error_code preset_unavailable", async () => {
      const noDefault: Presets = {
        builder: [{ id: "b-openai", agent_tool_id: 2, provider: "openai" }],
      };
      const { logger, calls } = makeFakeLogger();
      await resolvePresetHandler(logger, { preset: "builder" }, noDefault, {
        isProviderEnabled: disabledSet("openai"),
      });
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionFailure");
      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_preset", "builder");
      expect(fields).toHaveProperty("error_code", "preset_unavailable");
    });
  });
});
