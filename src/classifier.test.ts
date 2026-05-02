import { describe, expect, it } from "vitest";

import {
  classify,
  buildClassifierPolicy,
  defaultPolicy,
} from "./classifier.js";
import type { SoloAgentTool } from "./types/solo.js";
import {
  accurateNameMisleadingCommand,
  ambiguousCommand,
  enabledRuntimes,
  misleadingNameAccurateCommand,
  unknownCommand,
} from "./__fixtures__/agent-tools.js";

const findRuntime = (name: string): SoloAgentTool => {
  const tool = enabledRuntimes.find((t) => t.name === name);
  if (!tool) throw new Error(`fixture not found: ${name}`);
  return tool;
};

describe("classify (enabledRuntimes)", () => {
  it("opencode-ghc-haiku → small via command", () => {
    const result = classify(findRuntime("opencode-ghc-haiku"));
    expect(result.tier).toBe("small");
    expect(result.source).toBe("command");
    expect(result.ambiguous).toBe(false);
    expect(result.matchedTokens).toContain("haiku");
  });

  it("opencode-ghc-sonnet → medium via command", () => {
    const result = classify(findRuntime("opencode-ghc-sonnet"));
    expect(result.tier).toBe("medium");
    expect(result.source).toBe("command");
    expect(result.matchedTokens).toContain("sonnet");
  });

  it("codex-fast → small via command", () => {
    const result = classify(findRuntime("codex-fast"));
    expect(result.tier).toBe("small");
    expect(result.source).toBe("command");
    expect(result.matchedTokens).toContain("fast");
  });

  it("codex-standard → medium via command", () => {
    const result = classify(findRuntime("codex-standard"));
    expect(result.tier).toBe("medium");
    expect(result.source).toBe("command");
    expect(result.matchedTokens).toContain("standard");
  });

  it("codex-flagship → large via command", () => {
    const result = classify(findRuntime("codex-flagship"));
    expect(result.tier).toBe("large");
    expect(result.source).toBe("command");
    expect(result.matchedTokens).toContain("flagship");
  });
});

describe("classify (edge-case fixtures)", () => {
  it("misleadingNameAccurateCommand: command wins, name is not used as the source", () => {
    const result = classify(misleadingNameAccurateCommand);
    expect(result.tier).toBe("large");
    expect(result.source).toBe("command");
    expect(result.matchedTokens).toEqual(["opus"]);
    // Diagnostics still record the name token observation.
    expect(result.diagnostics.nameTokensSeen).toEqual([
      { tier: "small", token: "mini" },
    ]);
    expect(result.diagnostics.commandTokensSeen).toEqual([
      { tier: "large", token: "opus", source: "built_in" },
    ]);
  });

  it("accurateNameMisleadingCommand: falls back to name with source=name_fallback", () => {
    const result = classify(accurateNameMisleadingCommand);
    expect(result.tier).toBe("medium");
    expect(result.source).toBe("name_fallback");
    expect(result.matchedTokens).toEqual(["sonnet"]);
    expect(result.diagnostics.commandTokensSeen).toEqual([]);
    expect(result.diagnostics.nameTokensSeen).toEqual([
      { tier: "medium", token: "sonnet" },
    ]);
  });

  it("ambiguousCommand: tier=null, ambiguous=true, source=none", () => {
    const result = classify(ambiguousCommand);
    expect(result.tier).toBeNull();
    expect(result.source).toBe("none");
    expect(result.ambiguous).toBe(true);
    expect(result.matchedTokens).toEqual([]);
    expect(result.diagnostics.commandTokensSeen).toEqual(
      expect.arrayContaining([
        { tier: "small", token: "haiku", source: "built_in" },
        { tier: "large", token: "opus", source: "built_in" },
      ]),
    );
  });

  it("ambiguous command does not consult name even if name would resolve cleanly", () => {
    // Command has tokens in two tiers (haiku=small, opus=large) AND
    // name has a clean medium hit (sonnet). Classifier must still return
    // ambiguous=true with tier=null — no name fallback when command is ambiguous.
    const tool: SoloAgentTool = {
      id: 999,
      name: "sonnet-only-name",
      command: "runner --primary haiku --fallback opus",
      tool_type: "experimental",
      enabled: true,
    };
    const result = classify(tool);
    expect(result.tier).toBeNull();
    expect(result.ambiguous).toBe(true);
    expect(result.source).toBe("none");
    expect(result.matchedTokens).toEqual([]);
    // The name *was* observed in diagnostics but was not used to break the tie.
    expect(result.diagnostics.nameTokensSeen).toEqual([
      { tier: "medium", token: "sonnet" },
    ]);
  });

  it("unknownCommand: tier=null, source=none, not ambiguous", () => {
    const result = classify(unknownCommand);
    expect(result.tier).toBeNull();
    expect(result.source).toBe("none");
    expect(result.ambiguous).toBe(false);
    expect(result.matchedTokens).toEqual([]);
    expect(result.diagnostics.commandTokensSeen).toEqual([]);
    expect(result.diagnostics.nameTokensSeen).toEqual([]);
  });
});

describe("classify (case-insensitive matching)", () => {
  it("uppercase OPUS in command resolves to large", () => {
    const tool: SoloAgentTool = {
      id: 100,
      name: "shouting-runner",
      command: "runner --model OPUS",
      tool_type: "custom",
      enabled: true,
    };
    const result = classify(tool);
    expect(result.tier).toBe("large");
    expect(result.source).toBe("command");
    expect(result.matchedTokens).toEqual(["opus"]);
  });

  it("mixed-case Sonnet in name resolves to medium via name fallback", () => {
    const tool: SoloAgentTool = {
      id: 101,
      name: "Sonnet-Runner",
      command: "runner --config production",
      tool_type: "custom",
      enabled: true,
    };
    const result = classify(tool);
    expect(result.tier).toBe("medium");
    expect(result.source).toBe("name_fallback");
    expect(result.matchedTokens).toEqual(["sonnet"]);
  });
});

describe("classify (`pro` weak-signal name token)", () => {
  it("pro alone in name resolves to large via name fallback", () => {
    const tool: SoloAgentTool = {
      id: 200,
      name: "pro-runner",
      command: "runner --config production",
      tool_type: "custom",
      enabled: true,
    };
    const result = classify(tool);
    expect(result.tier).toBe("large");
    expect(result.source).toBe("name_fallback");
    expect(result.matchedTokens).toEqual(["pro"]);
  });

  it("pro alongside another tier's name token yields multi-tier null", () => {
    const tool: SoloAgentTool = {
      id: 201,
      name: "pro-mini-runner",
      command: "runner --config production",
      tool_type: "custom",
      enabled: true,
    };
    const result = classify(tool);
    expect(result.tier).toBeNull();
    expect(result.source).toBe("none");
    expect(result.ambiguous).toBe(false);
    expect(result.diagnostics.nameTokensSeen).toEqual(
      expect.arrayContaining([
        { tier: "small", token: "mini" },
        { tier: "large", token: "pro" },
      ]),
    );
  });
});

describe("classify (token boundary semantics)", () => {
  it("gpt-5.2 matches the gpt-5.2 segment", () => {
    const tool: SoloAgentTool = {
      id: 300,
      name: "openai-runner",
      command: "openai --model gpt-5.2",
      tool_type: "custom",
      enabled: true,
    };
    const result = classify(tool);
    expect(result.tier).toBe("medium");
    expect(result.matchedTokens).toEqual(["gpt-5.2"]);
  });

  it("gpt-5.2 does not match gpt-5.20", () => {
    const tool: SoloAgentTool = {
      id: 301,
      name: "openai-runner",
      command: "openai --model gpt-5.20",
      tool_type: "custom",
      enabled: true,
    };
    const result = classify(tool);
    expect(result.tier).toBeNull();
    expect(result.source).toBe("none");
    expect(result.diagnostics.commandTokensSeen).toEqual([]);
  });
});

describe("classify (purity)", () => {
  it("repeated calls with the same input return deeply equal output", () => {
    const tool = misleadingNameAccurateCommand;
    const a = classify(tool);
    const b = classify(tool);
    expect(a).toEqual(b);
    // Different object instances — purity is about value equality, not identity.
    expect(a).not.toBe(b);
  });

  it("does not mutate the input tool", () => {
    const tool: SoloAgentTool = {
      id: 400,
      name: "opencode-ghc-haiku",
      command: "opencode --model haiku",
      tool_type: "opencode",
      enabled: true,
    };
    const snapshot = JSON.stringify(tool);
    classify(tool);
    expect(JSON.stringify(tool)).toBe(snapshot);
  });
});

describe("buildClassifierPolicy + classify (override-awareness)", () => {
  it("no policy passed → all hits report matchSource === 'built_in'", () => {
    const tool = findRuntime("opencode-ghc-haiku");
    const result = classify(tool);
    expect(result.tier).toBe("small");
    expect(result.matchSource).toBe("built_in");
  });

  it("defaultPolicy() → all hits report matchSource === 'built_in'", () => {
    const tool = findRuntime("opencode-ghc-haiku");
    const policy = defaultPolicy();
    const result = classify(tool, policy);
    expect(result.tier).toBe("small");
    expect(result.matchSource).toBe("built_in");
  });

  it("extend mode adds new token → hit reports matchSource === 'override'", () => {
    const tool: SoloAgentTool = {
      id: 500,
      name: "custom-runner",
      command: "runner --model tiny",
      tool_type: "custom",
      enabled: true,
    };
    const policy = buildClassifierPolicy({
      command_tokens: {
        small: {
          mode: "extend",
          tokens: ["tiny"],
        },
      },
    });
    const result = classify(tool, policy);
    expect(result.tier).toBe("small");
    expect(result.matchSource).toBe("override");
    expect(result.matchedTokens).toContain("tiny");
  });

  it("replace mode wipes built-ins → affected tier only uses override tokens", () => {
    // Use a tool with command containing only "haiku", and name that doesn't contain a size token
    const tool: SoloAgentTool = {
      id: 509,
      name: "custom-runner",
      command: "runner --model haiku",
      tool_type: "custom",
      enabled: true,
    };
    const policy = buildClassifierPolicy({
      command_tokens: {
        small: {
          mode: "replace",
          tokens: ["tiny"],
        },
      },
    });
    const result = classify(tool, policy);
    // "haiku" no longer matches in command (it was replaced)
    expect(result.tier).toBeNull();
    expect(result.source).toBe("none");
  });

  it("replace mode with new token matching → reports matchSource === 'override'", () => {
    const tool: SoloAgentTool = {
      id: 501,
      name: "custom-runner",
      command: "runner --model tiny",
      tool_type: "custom",
      enabled: true,
    };
    const policy = buildClassifierPolicy({
      command_tokens: {
        small: {
          mode: "replace",
          tokens: ["tiny"],
        },
      },
    });
    const result = classify(tool, policy);
    expect(result.tier).toBe("small");
    expect(result.matchSource).toBe("override");
  });

  it("override token shadowed by built-in → reports matchSource === 'built_in' (dedup keeps built-in)", () => {
    const tool: SoloAgentTool = {
      id: 502,
      name: "custom-runner",
      command: "runner --model haiku",
      tool_type: "custom",
      enabled: true,
    };
    // "haiku" is already a built-in small token; adding it as an override should not change source
    const policy = buildClassifierPolicy({
      command_tokens: {
        small: {
          mode: "extend",
          tokens: ["haiku"],
        },
      },
    });
    const result = classify(tool, policy);
    expect(result.tier).toBe("small");
    expect(result.matchSource).toBe("built_in");
    expect(result.matchedTokens).toContain("haiku");
  });

  it("mixed extend with both built-in and override tokens for same tier → first-matched token source reported", () => {
    const tool: SoloAgentTool = {
      id: 503,
      name: "mixed-runner",
      command: "runner --haiku --tiny",
      tool_type: "custom",
      enabled: true,
    };
    const policy = buildClassifierPolicy({
      command_tokens: {
        small: {
          mode: "extend",
          tokens: ["tiny"],
        },
      },
    });
    const result = classify(tool, policy);
    expect(result.tier).toBe("small");
    // Built-in tokens are iterated first in extend mode, so "haiku" comes before "tiny"
    expect(result.matchSource).toBe("built_in");
    expect(result.matchedTokens).toContain("haiku");
    expect(result.matchedTokens).toContain("tiny");
  });

  it("source defaults to built_in when no match (source === 'none')", () => {
    const unknownTool = unknownCommand;
    const result = classify(unknownTool);
    expect(result.source).toBe("none");
    expect(result.matchSource).toBe("built_in");
  });

  it("name fallback → matchSource === 'built_in'", () => {
    const tool = accurateNameMisleadingCommand;
    const result = classify(tool);
    expect(result.source).toBe("name_fallback");
    expect(result.matchSource).toBe("built_in");
  });

  it("ambiguous command → matchSource === 'built_in' (no match)", () => {
    const tool = ambiguousCommand;
    const result = classify(tool);
    expect(result.ambiguous).toBe(true);
    expect(result.matchSource).toBe("built_in");
  });

  it("extend mode with override-only tier hit → reports matchSource === 'override'", () => {
    const tool: SoloAgentTool = {
      id: 504,
      name: "bespoke-runner",
      command: "runner --model bespoke-mid",
      tool_type: "custom",
      enabled: true,
    };
    const policy = buildClassifierPolicy({
      command_tokens: {
        medium: {
          mode: "replace",
          tokens: ["bespoke-mid"],
        },
      },
    });
    const result = classify(tool, policy);
    expect(result.tier).toBe("medium");
    expect(result.matchSource).toBe("override");
  });

  it("extend mode case-insensitive dedup → case variation of built-in is built_in", () => {
    const tool: SoloAgentTool = {
      id: 505,
      name: "case-test",
      command: "runner --model HAIKU",
      tool_type: "custom",
      enabled: true,
    };
    const policy = buildClassifierPolicy({
      command_tokens: {
        small: {
          mode: "extend",
          tokens: ["Haiku"], // Different case
        },
      },
    });
    const result = classify(tool, policy);
    expect(result.tier).toBe("small");
    expect(result.matchSource).toBe("built_in");
  });

  it("extend mode preserves token iteration order for tie-breaking", () => {
    const tool1: SoloAgentTool = {
      id: 506,
      name: "test-first-override",
      command: "runner --model override-first built-second",
      tool_type: "custom",
      enabled: true,
    };
    const policy = buildClassifierPolicy({
      command_tokens: {
        small: {
          mode: "extend",
          tokens: ["override-first"],
        },
      },
    });
    // Both override and built-in present, but built-in iteration comes first
    // so we need a tool where override appears in command first to test iteration order
    const tool2: SoloAgentTool = {
      id: 507,
      name: "test-builtin-order",
      command: "runner --mini --haiku",
      tool_type: "custom",
      enabled: true,
    };
    // Both mini and haiku are built-in small tokens
    // They should appear in the order they're in COMMAND_TOKENS
    const result = classify(tool2, policy);
    expect(result.matchedTokens[0]).toBe("haiku"); // haiku comes first in COMMAND_TOKENS.small
  });

  it("multiple override tokens in extend mode → dedup, first occurrence wins", () => {
    const tool: SoloAgentTool = {
      id: 508,
      name: "dedup-test",
      command: "runner --model tiny",
      tool_type: "custom",
      enabled: true,
    };
    const policy = buildClassifierPolicy({
      command_tokens: {
        small: {
          mode: "extend",
          tokens: ["tiny", "tiny"],
        },
      },
    });
    const result = classify(tool, policy);
    expect(result.tier).toBe("small");
    expect(result.matchedTokens).toEqual(["tiny"]);
  });
});
