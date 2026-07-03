import { describe, expect, it } from "vitest";

import { parseConfig } from "./config";

describe("parseConfig", () => {
  it("parses valid config", () => {
    const config = parseConfig({
      solo: {
        transport: {
          type: "stdio",
          command: "solo",
          args: ["mcp", "serve"],
        },
      },
    });

    expect(config.solo.transport.command).toBe("solo");
    expect(config.solo.transport.args).toEqual(["mcp", "serve"]);
  });

  it("rejects deprecated solo.processId field (strict schema)", () => {
    expect(() =>
      parseConfig({
        solo: {
          transport: { type: "stdio", command: "solo" },
          processId: "anything",
        },
      }),
    ).toThrow(/processId|unrecognized/i);
  });

  it("rejects deprecated solo.projectId field (strict schema)", () => {
    expect(() =>
      parseConfig({
        solo: {
          transport: { type: "stdio", command: "solo" },
          projectId: "anything",
        },
      }),
    ).toThrow(/projectId|unrecognized/i);
  });

  it("accepts config without command (command is optional)", () => {
    const config = parseConfig({
      solo: {
        transport: {
          type: "stdio",
        },
      },
    });
    expect(config.solo.transport.command).toBeUndefined();
  });

  it("throws field-level error for invalid field type", () => {
    expect(() =>
      parseConfig({
        solo: {
          transport: {
            type: "stdio",
            command: 42,
          },
        },
      }),
    ).toThrow("solo.transport.command");
  });

  describe("presets field", () => {
    it("parses config without presets block: presets is undefined", () => {
      const config = parseConfig({
        solo: {
          transport: { type: "stdio", command: "solo" },
        },
      });

      expect(config.presets).toBeUndefined();
    });

    it("parses config with a valid presets block", () => {
      const config = parseConfig({
        solo: {
          transport: { type: "stdio", command: "solo" },
        },
        presets: {
          builder: [
            {
              id: "abc123xy",
              agent_tool_id: 4,
              extra_args: "-m sonnet",
              provider: "anthropic",
            },
          ],
          default: [{ id: "def456uv", agent_tool_id: 4 }],
        },
      });

      expect(config.presets?.builder).toHaveLength(1);
      expect(config.presets?.builder?.[0]?.agent_tool_id).toBe(4);
      expect(config.presets?.default?.[0]?.provider).toBeUndefined();
    });

    it("throws for a definition with an extra key (strict)", () => {
      expect(() =>
        parseConfig({
          solo: {
            transport: { type: "stdio", command: "solo" },
          },
          presets: {
            builder: [{ id: "abc123xy", agent_tool_id: 4, bogus: true }],
          },
        }),
      ).toThrow(/presets|builder|unrecognized/i);
    });
  });
});
