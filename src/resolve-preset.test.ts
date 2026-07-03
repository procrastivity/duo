import { describe, expect, it } from "vitest";

import { PresetUnavailableError, UnknownPresetError } from "./errors.js";
import { resolvePreset } from "./resolver.js";
import type { Presets } from "./types/presets.js";

// Deterministic rng cycling through a fixed sequence; `Math.floor(v * n)` maps
// each value to a pool index.
const seededRng = (sequence: number[]): (() => number) => {
  let i = 0;
  return () => {
    const v = sequence[i % sequence.length]!;
    i += 1;
    return v;
  };
};

// Injected `isProviderEnabled` overrides — no filesystem (OQ6).
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
  default: [{ id: "d-anthropic", agent_tool_id: 10, provider: "anthropic" }],
};

describe("resolvePreset — unknown preset", () => {
  it("throws UnknownPresetError before any selection work", () => {
    expect(() =>
      resolvePreset(presets, "nope", { isProviderEnabled: allEnabled }),
    ).toThrow(UnknownPresetError);
    try {
      resolvePreset(presets, "nope", { isProviderEnabled: allEnabled });
    } catch (err) {
      expect(err).toBeInstanceOf(UnknownPresetError);
      expect((err as UnknownPresetError).preset).toBe("nope");
    }
  });
});

describe("resolvePreset — uniform random pick, all enabled", () => {
  it("picks the pool index indicated by rng", () => {
    const result = resolvePreset(presets, "builder", {
      isProviderEnabled: allEnabled,
      rng: seededRng([0.4]), // floor(0.4 * 3) === 1 → second def
    });
    expect(result.agent_tool_id).toBe(2);
    expect(result.provider).toBe("openai");
    expect(result.preset_requested).toBe("builder");
    expect(result.preset_used).toBe("builder");
    expect(result.fell_back_to_default).toBe(false);
    expect(result.relented_on_avoid_provider).toBe(false);
  });

  it("reaches every definition across seeded rng values", () => {
    const seen = new Set<number>();
    for (const v of [0, 0.4, 0.8]) {
      const result = resolvePreset(presets, "builder", {
        isProviderEnabled: allEnabled,
        rng: seededRng([v]),
      });
      seen.add(result.agent_tool_id);
    }
    expect(seen).toEqual(new Set([1, 2, 3]));
  });

  it("clamps an rng that returns 1 to the last index", () => {
    const result = resolvePreset(presets, "builder", {
      isProviderEnabled: allEnabled,
      rng: seededRng([1]),
    });
    expect(result.agent_tool_id).toBe(3);
  });
});

describe("resolvePreset — eligibility with disabled providers", () => {
  it("excludes disabled-provider defs; no-provider def is always eligible", () => {
    // openai disabled ⇒ pool is {anthropic(1), local(3)}.
    const seen = new Set<number>();
    for (const v of [0, 0.6]) {
      const result = resolvePreset(presets, "builder", {
        isProviderEnabled: disabledSet("openai"),
        rng: seededRng([v]),
      });
      seen.add(result.agent_tool_id);
    }
    expect(seen).toEqual(new Set([1, 3]));
  });

  it("keeps a no-provider def eligible even when all named providers are disabled", () => {
    const result = resolvePreset(presets, "builder", {
      isProviderEnabled: disabledSet("anthropic", "openai"),
      rng: seededRng([0]),
    });
    expect(result.agent_tool_id).toBe(3); // the only survivor: b-local
    expect(result.provider).toBeUndefined();
  });
});

describe("resolvePreset — default fallback", () => {
  it("falls back to default when the requested preset has no eligible def", () => {
    const openaiOnly: Presets = {
      openaionly: [{ id: "o", agent_tool_id: 5, provider: "openai" }],
      default: [{ id: "d", agent_tool_id: 10, provider: "anthropic" }],
    };
    const result = resolvePreset(openaiOnly, "openaionly", {
      isProviderEnabled: disabledSet("openai"),
      rng: seededRng([0]),
    });
    expect(result.agent_tool_id).toBe(10);
    expect(result.preset_used).toBe("default");
    expect(result.fell_back_to_default).toBe(true);
    expect(result.relented_on_avoid_provider).toBe(false);
  });
});

describe("resolvePreset — unavailable", () => {
  it("throws PresetUnavailableError naming disabled providers when nothing survives", () => {
    const p: Presets = {
      solo: [{ id: "s", agent_tool_id: 5, provider: "openai" }],
      default: [{ id: "d", agent_tool_id: 10, provider: "gemini" }],
    };
    try {
      resolvePreset(p, "solo", {
        isProviderEnabled: disabledSet("openai", "gemini"),
        rng: seededRng([0]),
      });
      throw new Error("expected PresetUnavailableError");
    } catch (err) {
      expect(err).toBeInstanceOf(PresetUnavailableError);
      const diag = (err as PresetUnavailableError).diagnostics;
      expect(diag.requested_preset).toBe("solo");
      expect(diag.default_present).toBe(true);
      expect(diag.default_tried).toBe(true);
      expect(diag.disabled_providers).toEqual(["gemini", "openai"]);
      expect(diag.avoid_provider).toBeUndefined();
    }
  });

  it("reports default_present=false when there is no default preset", () => {
    const p: Presets = {
      solo: [{ id: "s", agent_tool_id: 5, provider: "openai" }],
    };
    try {
      resolvePreset(p, "solo", { isProviderEnabled: disabledSet("openai") });
      throw new Error("expected PresetUnavailableError");
    } catch (err) {
      expect(err).toBeInstanceOf(PresetUnavailableError);
      const diag = (err as PresetUnavailableError).diagnostics;
      expect(diag.default_present).toBe(false);
      expect(diag.default_tried).toBe(false);
      expect(diag.disabled_providers).toEqual(["openai"]);
    }
  });
});

describe("resolvePreset — avoid_provider (soft)", () => {
  it("(i) picks an enabled ≠avoid def without relenting", () => {
    // avoid openai ⇒ pool is {anthropic(1), local(3)}, openai(2) excluded.
    const seen = new Set<number>();
    for (const v of [0, 0.6]) {
      const result = resolvePreset(presets, "builder", {
        isProviderEnabled: allEnabled,
        avoidProvider: "openai",
        rng: seededRng([v]),
      });
      expect(result.relented_on_avoid_provider).toBe(false);
      seen.add(result.agent_tool_id);
    }
    expect(seen).toEqual(new Set([1, 3]));
  });

  it("(ii) honors ≠avoid via the default preset before relenting", () => {
    const p: Presets = {
      openaionly: [{ id: "o", agent_tool_id: 5, provider: "openai" }],
      default: [{ id: "d", agent_tool_id: 10, provider: "anthropic" }],
    };
    const result = resolvePreset(p, "openaionly", {
      isProviderEnabled: allEnabled,
      avoidProvider: "openai",
      rng: seededRng([0]),
    });
    expect(result.agent_tool_id).toBe(10);
    expect(result.preset_used).toBe("default");
    expect(result.fell_back_to_default).toBe(true);
    expect(result.relented_on_avoid_provider).toBe(false);
  });

  it("(iii) relents onto the avoided provider rather than failing", () => {
    const p: Presets = {
      openaionly: [{ id: "o", agent_tool_id: 5, provider: "openai" }],
    };
    const result = resolvePreset(p, "openaionly", {
      isProviderEnabled: allEnabled,
      avoidProvider: "openai",
      rng: seededRng([0]),
    });
    expect(result.agent_tool_id).toBe(5);
    expect(result.provider).toBe("openai");
    expect(result.relented_on_avoid_provider).toBe(true);
    expect(result.fell_back_to_default).toBe(false);
  });

  it("(iv) never hard-fails on avoid_provider alone", () => {
    // Only the avoided provider is available and enabled — must still resolve.
    const p: Presets = {
      solo: [{ id: "s", agent_tool_id: 7, provider: "openai" }],
    };
    expect(() =>
      resolvePreset(p, "solo", {
        isProviderEnabled: allEnabled,
        avoidProvider: "openai",
        rng: seededRng([0]),
      }),
    ).not.toThrow();
  });
});

describe("resolvePreset — extra_args", () => {
  it("tokenizes a raw extra_args string", () => {
    const p: Presets = {
      solo: [
        {
          id: "s",
          agent_tool_id: 1,
          extra_args: "--model sonnet --prompt 'hello world'",
        },
      ],
    };
    const result = resolvePreset(p, "solo", {
      isProviderEnabled: allEnabled,
      rng: seededRng([0]),
    });
    expect(result.extra_args).toEqual([
      "--model",
      "sonnet",
      "--prompt",
      "hello world",
    ]);
  });

  it("returns [] when the definition has no extra_args", () => {
    const p: Presets = {
      solo: [{ id: "s", agent_tool_id: 1 }],
    };
    const result = resolvePreset(p, "solo", {
      isProviderEnabled: allEnabled,
      rng: seededRng([0]),
    });
    expect(result.extra_args).toEqual([]);
  });
});
