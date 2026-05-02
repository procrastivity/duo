import { describe, expect, it } from "vitest";

import { classify } from "./classifier.js";
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
      { tier: "large", token: "opus" },
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
        { tier: "small", token: "haiku" },
        { tier: "large", token: "opus" },
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
