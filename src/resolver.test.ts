import { describe, expect, it } from "vitest";

import {
  TierUnavailableError,
  UnsupportedTierError,
} from "./errors.js";
import { resolveAgentTool } from "./resolver.js";
import type { SoloAgentTool } from "./types/solo.js";
import {
  accurateNameMisleadingCommand,
  ambiguousCommand,
  disabledVariants,
  enabledRuntimes,
  misleadingNameAccurateCommand,
  unknownCommand,
} from "./__fixtures__/agent-tools.js";

const seededRng = (sequence: number[]): (() => number) => {
  let i = 0;
  return () => {
    const v = sequence[i % sequence.length]!;
    i += 1;
    return v;
  };
};

const findById = (tools: SoloAgentTool[], id: number): SoloAgentTool => {
  const t = tools.find((x) => x.id === id);
  if (!t) throw new Error(`fixture not found: id=${id}`);
  return t;
};

describe("resolveAgentTool — happy path against enabledRuntimes", () => {
  it("small tier resolves to a small candidate (haiku or fast)", () => {
    const result = resolveAgentTool(enabledRuntimes, "small", {
      rng: seededRng([0]),
    });
    expect(["opencode-ghc-haiku", "codex-fast"]).toContain(
      result.selected.tool_name,
    );
    expect(result.diagnostics.requested_tier).toBe("small");
    expect(result.diagnostics.candidates_considered).toBe(2);
    expect(result.diagnostics.strategy).toBe("random");
  });

  it("medium tier resolves to a medium candidate (sonnet or standard)", () => {
    const result = resolveAgentTool(enabledRuntimes, "medium", {
      rng: seededRng([0]),
    });
    expect(["opencode-ghc-sonnet", "codex-standard"]).toContain(
      result.selected.tool_name,
    );
    expect(result.diagnostics.candidates_considered).toBe(2);
  });

  it("large tier resolves to codex-flagship (only large candidate)", () => {
    const result = resolveAgentTool(enabledRuntimes, "large", {
      rng: seededRng([0]),
    });
    expect(result.selected.tool_name).toBe("codex-flagship");
    expect(result.selected.agent_tool_id).toBe(5);
    expect(result.classification_source).toBe("command");
    expect(result.matched_tokens).toContain("flagship");
    expect(result.alternatives).toEqual([]);
    expect(result.diagnostics.candidates_considered).toBe(1);
  });

  it("populates total_tools and enabled_count for full enabled list", () => {
    const result = resolveAgentTool(enabledRuntimes, "large", {
      rng: seededRng([0]),
    });
    expect(result.diagnostics.total_tools).toBe(enabledRuntimes.length);
    expect(result.diagnostics.enabled_count).toBe(enabledRuntimes.length);
    expect(result.diagnostics.excluded_count).toBe(0);
  });
});

describe("resolveAgentTool — disabled tools are dropped before classification", () => {
  it("disabledVariants alone yield TierUnavailableError with enabled_count=0", () => {
    expect.assertions(4);
    try {
      resolveAgentTool(disabledVariants, "small");
    } catch (err) {
      expect(err).toBeInstanceOf(TierUnavailableError);
      const e = err as TierUnavailableError;
      expect(e.diagnostics.enabled_count).toBe(0);
      expect(e.diagnostics.total_tools).toBe(disabledVariants.length);
      // No tool was classified, so ambiguous/unclassifiable counts are 0.
      expect(e.diagnostics.ambiguous_count + e.diagnostics.unclassifiable_count).toBe(0);
    }
  });

  it("mixing enabled + disabled yields candidates only from the enabled set", () => {
    const tools: SoloAgentTool[] = [
      ...enabledRuntimes,
      ...disabledVariants.map((t) => ({ ...t, id: t.id + 100 })),
    ];
    const result = resolveAgentTool(tools, "small", { rng: seededRng([0]) });
    expect(result.diagnostics.total_tools).toBe(tools.length);
    expect(result.diagnostics.enabled_count).toBe(enabledRuntimes.length);
    // Only the two enabled small tools (haiku, fast) are considered.
    expect(result.diagnostics.candidates_considered).toBe(2);
    const allCandidateIds = [
      result.selected.agent_tool_id,
      ...result.alternatives.map((a) => a.agent_tool_id),
    ];
    // Every selected/alt id must come from the enabled set (id 1..5).
    for (const id of allCandidateIds) {
      expect(id).toBeLessThanOrEqual(5);
    }
  });
});

describe("resolveAgentTool — classification source surfaces correctly", () => {
  it("misleadingNameAccurateCommand resolves on command", () => {
    const tools = [...enabledRuntimes, misleadingNameAccurateCommand];
    const result = resolveAgentTool(tools, "large", { rng: seededRng([0]) });
    // Two large candidates now: codex-flagship (5) and misleadingNameAccurateCommand (10).
    expect(result.diagnostics.candidates_considered).toBe(2);
    const allIds = [
      result.selected.agent_tool_id,
      ...result.alternatives.map((a) => a.agent_tool_id),
    ];
    expect(allIds).toContain(10);

    // Force selection of id=10 via rng=0.5 (sorted candidates: [5,10], idx=1).
    const forced = resolveAgentTool(tools, "large", {
      rng: seededRng([0.99]),
    });
    // With rng near 1, idx = floor(0.99 * 2) = 1; candidate ordering matches insertion order
    // (5 came first via enabledRuntimes, 10 appended). So selected should be id=10.
    expect(forced.selected.agent_tool_id).toBe(10);
    expect(forced.classification_source).toBe("command");
    expect(forced.matched_tokens).toContain("opus");
  });

  it("accurateNameMisleadingCommand resolves via name_fallback", () => {
    const tools = [accurateNameMisleadingCommand];
    const result = resolveAgentTool(tools, "medium", { rng: seededRng([0]) });
    expect(result.selected.agent_tool_id).toBe(11);
    expect(result.classification_source).toBe("name_fallback");
    expect(result.matched_tokens).toContain("sonnet");
  });
});

describe("resolveAgentTool — ambiguous and unclassifiable diagnostics", () => {
  it("ambiguousCommand increments ambiguous_count and is not a candidate", () => {
    const tools = [...enabledRuntimes, ambiguousCommand];
    const result = resolveAgentTool(tools, "large", { rng: seededRng([0]) });
    expect(result.diagnostics.ambiguous_count).toBe(1);
    const allIds = [
      result.selected.agent_tool_id,
      ...result.alternatives.map((a) => a.agent_tool_id),
    ];
    expect(allIds).not.toContain(ambiguousCommand.id);
    // Same assertion against small (where one of the ambiguous tokens, "haiku", lives).
    const small = resolveAgentTool(tools, "small", { rng: seededRng([0]) });
    expect(small.diagnostics.ambiguous_count).toBe(1);
    const smallIds = [
      small.selected.agent_tool_id,
      ...small.alternatives.map((a) => a.agent_tool_id),
    ];
    expect(smallIds).not.toContain(ambiguousCommand.id);
  });

  it("unknownCommand increments unclassifiable_count and is not a candidate", () => {
    const tools = [...enabledRuntimes, unknownCommand];
    const result = resolveAgentTool(tools, "medium", { rng: seededRng([0]) });
    expect(result.diagnostics.unclassifiable_count).toBe(1);
    const allIds = [
      result.selected.agent_tool_id,
      ...result.alternatives.map((a) => a.agent_tool_id),
    ];
    expect(allIds).not.toContain(unknownCommand.id);
  });
});

describe("resolveAgentTool — UnsupportedTierError", () => {
  it.each(["giant", "", "SMALL", "Medium", "tiny"])(
    "throws for tier label %j",
    (label) => {
      expect(() => resolveAgentTool(enabledRuntimes, label)).toThrow(
        UnsupportedTierError,
      );
    },
  );

  it("error carries offending label and canonical tier list", () => {
    try {
      resolveAgentTool(enabledRuntimes, "giant");
      throw new Error("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(UnsupportedTierError);
      const e = err as UnsupportedTierError;
      expect(e.code).toBe("unsupported_tier");
      expect(e.requested).toBe("giant");
      expect(e.supported).toEqual(["small", "medium", "large"]);
    }
  });
});

describe("resolveAgentTool — TierUnavailableError", () => {
  it("empty input list throws with diagnostics", () => {
    try {
      resolveAgentTool([], "small");
      throw new Error("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(TierUnavailableError);
      const e = err as TierUnavailableError;
      expect(e.code).toBe("tier_unavailable");
      expect(e.diagnostics.requested_tier).toBe("small");
      expect(e.diagnostics.total_tools).toBe(0);
      expect(e.diagnostics.enabled_count).toBe(0);
      expect(e.diagnostics.candidates_considered).toBe(0);
      expect(e.diagnostics.strategy).toBe("random");
      expect(e.diagnostics.ignored_tools).toEqual([]);
    }
  });

  it("requesting a tier nobody matches surfaces ignored-tool classifications", () => {
    // enabledRuntimes contain only small/medium/large; if we wipe out the small
    // entries, requesting small should fail and report the medium/large tools as
    // wrong-tier ignored entries.
    const onlyMediumLarge = enabledRuntimes.filter(
      (t) => t.id === 2 || t.id === 4 || t.id === 5,
    );
    try {
      resolveAgentTool(onlyMediumLarge, "small");
      throw new Error("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(TierUnavailableError);
      const e = err as TierUnavailableError;
      expect(e.diagnostics.candidates_considered).toBe(0);
      expect(e.diagnostics.ignored_tools).toHaveLength(3);
      const reasons = new Set(e.diagnostics.ignored_tools.map((x) => x.reason));
      expect(reasons).toEqual(new Set(["wrong_tier"]));
      const detectedTiers = e.diagnostics.ignored_tools.map(
        (x) => x.detected_tier,
      );
      expect(detectedTiers.sort()).toEqual(["large", "medium", "medium"]);
    }
  });

  it("ambiguous-only and unknown-only inputs both throw with the correct counts", () => {
    try {
      resolveAgentTool([ambiguousCommand, unknownCommand], "small");
      throw new Error("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(TierUnavailableError);
      const e = err as TierUnavailableError;
      expect(e.diagnostics.ambiguous_count).toBe(1);
      expect(e.diagnostics.unclassifiable_count).toBe(1);
      expect(e.diagnostics.candidates_considered).toBe(0);
      const reasons = e.diagnostics.ignored_tools
        .map((x) => x.reason)
        .sort();
      expect(reasons).toEqual(["ambiguous", "unclassifiable"]);
    }
  });
});

describe("resolveAgentTool — selection determinism with seeded rng", () => {
  it("rng=0 picks the first candidate in input order; alternatives sorted by id ascending", () => {
    // Two small candidates: id=1 (haiku) and id=3 (fast). With rng=0, idx=0 picks id=1.
    const result = resolveAgentTool(enabledRuntimes, "small", {
      rng: seededRng([0]),
    });
    expect(result.selected.agent_tool_id).toBe(1);
    expect(result.alternatives.map((a) => a.agent_tool_id)).toEqual([3]);
  });

  it("rng=0.99 picks the last candidate in input order", () => {
    const result = resolveAgentTool(enabledRuntimes, "small", {
      rng: seededRng([0.99]),
    });
    expect(result.selected.agent_tool_id).toBe(3);
    expect(result.alternatives.map((a) => a.agent_tool_id)).toEqual([1]);
  });

  it("alternatives are id-sorted regardless of input order", () => {
    // Reverse the input so insertion order would put id=3 before id=1.
    const reversed = [...enabledRuntimes].reverse();
    const result = resolveAgentTool(reversed, "small", {
      rng: seededRng([0]),
    });
    // With rng=0 against reversed input, idx=0 picks the first candidate found.
    // Iteration over `reversed` finds id=3 first (codex-fast). Selected = 3.
    expect(result.selected.agent_tool_id).toBe(3);
    // Alternatives must be id-sorted ascending — so [1].
    expect(result.alternatives.map((a) => a.agent_tool_id)).toEqual([1]);
  });
});

describe("resolveAgentTool — multi-candidate across distinct tool_types", () => {
  it("small tier lists both opencode and codex tools as candidates", () => {
    const result = resolveAgentTool(enabledRuntimes, "small", {
      rng: seededRng([0]),
    });
    const allCandidates = [
      {
        id: result.selected.agent_tool_id,
        tool_type: result.selected.tool_type,
      },
      ...result.alternatives.map((a) => ({
        id: a.agent_tool_id,
        tool_type: a.tool_type,
      })),
    ];
    const types = new Set(allCandidates.map((c) => c.tool_type));
    expect(types).toEqual(new Set(["opencode", "codex"]));
    const ids = allCandidates.map((c) => c.id).sort((a, b) => a - b);
    expect(ids).toEqual([1, 3]);
  });
});

describe("resolveAgentTool — excludeIds", () => {
  it("excludeIds removes a matching tool from candidates and bumps excluded_count", () => {
    // Exclude id=1 (haiku) so only id=3 (fast) remains as a small candidate.
    const result = resolveAgentTool(enabledRuntimes, "small", {
      excludeIds: [1],
      rng: seededRng([0]),
    });
    expect(result.selected.agent_tool_id).toBe(3);
    expect(result.alternatives).toEqual([]);
    expect(result.diagnostics.excluded_count).toBe(1);
    expect(result.diagnostics.candidates_considered).toBe(1);
  });

  it("excluding all small tools forces TierUnavailableError", () => {
    try {
      resolveAgentTool(enabledRuntimes, "small", { excludeIds: [1, 3] });
      throw new Error("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(TierUnavailableError);
      const e = err as TierUnavailableError;
      expect(e.diagnostics.excluded_count).toBe(2);
      expect(e.diagnostics.candidates_considered).toBe(0);
    }
  });

  it("excludeIds matching disabled tools does not double-count", () => {
    // id=2 is enabled (sonnet); id=22 doesn't exist. Exclusion only counts enabled hits.
    const result = resolveAgentTool(enabledRuntimes, "medium", {
      excludeIds: [2, 999],
      rng: seededRng([0]),
    });
    expect(result.diagnostics.excluded_count).toBe(1);
    expect(result.selected.agent_tool_id).toBe(4);
  });

  it("excluding a disabled tool's id does not add to excluded_count", () => {
    // Build mixed list: include disabled variant of haiku at id=101.
    const tools: SoloAgentTool[] = [
      ...enabledRuntimes,
      { ...findById([...enabledRuntimes], 1), id: 101, enabled: false },
    ];
    const result = resolveAgentTool(tools, "small", {
      excludeIds: [101],
      rng: seededRng([0]),
    });
    // 101 was already filtered out by enabled=false, so it cannot also be excluded.
    expect(result.diagnostics.excluded_count).toBe(0);
    expect(result.diagnostics.enabled_count).toBe(enabledRuntimes.length);
  });
});

describe("resolveAgentTool — input immutability", () => {
  it("does not mutate the tools array or its elements", () => {
    const tools: SoloAgentTool[] = [
      ...enabledRuntimes,
      misleadingNameAccurateCommand,
      accurateNameMisleadingCommand,
      ambiguousCommand,
      unknownCommand,
    ];
    const beforeRefs = tools.slice();
    const beforeJson = JSON.stringify(tools);

    resolveAgentTool(tools, "small", { rng: seededRng([0]) });
    resolveAgentTool(tools, "medium", { rng: seededRng([0]) });
    resolveAgentTool(tools, "large", { rng: seededRng([0]) });

    expect(JSON.stringify(tools)).toBe(beforeJson);
    expect(tools).toEqual(beforeRefs);
    // Element identity preserved — no entries replaced in place.
    for (let i = 0; i < tools.length; i++) {
      expect(tools[i]).toBe(beforeRefs[i]);
    }
  });

  it("does not mutate options.excludeIds", () => {
    const excludeIds = [1, 3];
    const before = [...excludeIds];
    try {
      resolveAgentTool(enabledRuntimes, "small", { excludeIds });
    } catch {
      // Expected TierUnavailableError; we only care about the input array state.
    }
    expect(excludeIds).toEqual(before);
  });
});
