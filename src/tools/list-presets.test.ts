import { describe, expect, it } from "vitest";
import { listPresets } from "./list-presets.js";
import type { Presets } from "../types/presets.js";

const allEnabled = (): boolean => true;
const disabledSet =
  (...off: string[]) =>
  (provider: string): boolean =>
    !off.includes(provider);

const presets: Presets = {
  builder: [
    { id: "b-anthropic", agent_tool_id: 1, provider: "anthropic" },
    { id: "b-openai", agent_tool_id: 2, provider: "openai" },
    { id: "b-local", agent_tool_id: 3 },
  ],
  planner: [{ id: "p-openai", agent_tool_id: 5, provider: "openai" }],
  default: [{ id: "d-anthropic", agent_tool_id: 10, provider: "anthropic" }],
};

describe("listPresets — enumerates configured presets", () => {
  it("returns an entry for every configured preset", () => {
    const result = listPresets(presets, { isProviderEnabled: allEnabled });
    expect(Object.keys(result).sort()).toEqual(["builder", "default", "planner"]);
  });

  it("all presets available when every provider is enabled", () => {
    const result = listPresets(presets, { isProviderEnabled: allEnabled });
    expect(result.builder.available).toBe(true);
    expect(result.planner.available).toBe(true);
    expect(result.default.available).toBe(true);
  });

  it("surfaces each definition with its agent_tool_id, provider, and enabled flag", () => {
    const result = listPresets(presets, { isProviderEnabled: allEnabled });
    expect(result.builder.definitions).toEqual([
      { id: "b-anthropic", agent_tool_id: 1, provider: "anthropic", enabled: true },
      { id: "b-openai", agent_tool_id: 2, provider: "openai", enabled: true },
      { id: "b-local", agent_tool_id: 3, enabled: true },
    ]);
  });

  it("empty / undefined presets yields an empty result", () => {
    expect(listPresets(undefined)).toEqual({});
    expect(listPresets({})).toEqual({});
  });
});

describe("listPresets — provider disabled-state", () => {
  it("marks disabled-provider definitions as not enabled", () => {
    const result = listPresets(presets, {
      isProviderEnabled: disabledSet("openai"),
    });
    const openaiDef = result.builder.definitions.find(
      (d) => d.provider === "openai",
    );
    expect(openaiDef!.enabled).toBe(false);
    // builder still available: anthropic + local remain eligible.
    expect(result.builder.available).toBe(true);
  });

  it("a preset with only disabled providers is available via the default fallback", () => {
    // planner offers only openai (disabled); default (anthropic) is enabled.
    const result = listPresets(presets, {
      isProviderEnabled: disabledSet("openai"),
    });
    expect(result.planner.definitions[0].enabled).toBe(false);
    expect(result.planner.available).toBe(true);
  });

  it("a preset is unavailable when neither it nor default has an eligible def", () => {
    const result = listPresets(presets, {
      isProviderEnabled: disabledSet("openai", "anthropic"),
    });
    // planner: openai disabled; default: anthropic disabled → unavailable.
    expect(result.planner.available).toBe(false);
    // builder: local (no provider) still eligible → available.
    expect(result.builder.available).toBe(true);
  });

  it("default is not rescued by itself when its only provider is disabled", () => {
    const result = listPresets(presets, {
      isProviderEnabled: disabledSet("anthropic"),
    });
    expect(result.default.available).toBe(false);
  });
});
