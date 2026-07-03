import { describe, expect, it } from "vitest";

import { PresetDefinitionSchema, PresetsSchema } from "./presets";

describe("PresetDefinitionSchema", () => {
  it("parses a valid definition", () => {
    const result = PresetDefinitionSchema.parse({
      id: "abc123xy",
      agent_tool_id: 4,
      extra_args: "-m sonnet",
      provider: "anthropic",
    });

    expect(result).toEqual({
      id: "abc123xy",
      agent_tool_id: 4,
      extra_args: "-m sonnet",
      provider: "anthropic",
    });
  });

  it("parses a minimal definition (only id + agent_tool_id)", () => {
    const result = PresetDefinitionSchema.parse({
      id: "abc123xy",
      agent_tool_id: 17,
    });

    expect(result.extra_args).toBeUndefined();
    expect(result.provider).toBeUndefined();
  });

  it("rejects a missing id", () => {
    const result = PresetDefinitionSchema.safeParse({
      agent_tool_id: 4,
    });

    expect(result.success).toBe(false);
  });

  it("rejects an empty id", () => {
    const result = PresetDefinitionSchema.safeParse({
      id: "",
      agent_tool_id: 4,
    });

    expect(result.success).toBe(false);
  });

  it("rejects an unknown/extra key (strict)", () => {
    const result = PresetDefinitionSchema.safeParse({
      id: "abc123xy",
      agent_tool_id: 4,
      unexpected: "nope",
    });

    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues[0]?.code).toBe("unrecognized_keys");
    }
  });

  it("rejects a non-integer agent_tool_id", () => {
    const result = PresetDefinitionSchema.safeParse({
      id: "abc123xy",
      agent_tool_id: 4.5,
    });

    expect(result.success).toBe(false);
  });

  it("rejects an empty provider", () => {
    const result = PresetDefinitionSchema.safeParse({
      id: "abc123xy",
      agent_tool_id: 4,
      provider: "",
    });

    expect(result.success).toBe(false);
  });
});

describe("PresetsSchema", () => {
  it("parses presets mapping labels to definition arrays", () => {
    const result = PresetsSchema.parse({
      builder: [
        { id: "abc123xy", agent_tool_id: 4, extra_args: "-m sonnet", provider: "anthropic" },
        { id: "def456uv", agent_tool_id: 17, provider: "openrouter" },
      ],
      default: [{ id: "ghi789st", agent_tool_id: 4 }],
    });

    expect(result.builder).toHaveLength(2);
    expect(result.default?.[0]?.agent_tool_id).toBe(4);
  });

  it("rejects an empty preset name/label", () => {
    const result = PresetsSchema.safeParse({
      "": [{ id: "abc123xy", agent_tool_id: 4 }],
    });

    expect(result.success).toBe(false);
  });

  it("rejects a definition inside a preset that violates the definition schema", () => {
    const result = PresetsSchema.safeParse({
      builder: [{ agent_tool_id: 4 }],
    });

    expect(result.success).toBe(false);
  });
});
