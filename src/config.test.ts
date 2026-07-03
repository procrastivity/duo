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

  describe("policy field", () => {
    it("parses config without policy block: policy is undefined", () => {
      const config = parseConfig({
        solo: {
          transport: {
            type: "stdio",
            command: "solo",
            args: ["mcp", "serve"],
          },
        },
      });

      expect(config.policy).toBeUndefined();
    });

    it("parses config with valid policy block", () => {
      const config = parseConfig({
        solo: {
          transport: {
            type: "stdio",
            command: "solo",
            args: ["mcp", "serve"],
          },
        },
        policy: {
          command_tokens: {
            small: {
              mode: "extend",
              tokens: ["custom-small"],
            },
          },
          selection: {
            preference: [
              {
                tool_type: "test-type",
              },
            ],
          },
        },
      });

      expect(config.policy).toBeDefined();
      expect(config.policy?.command_tokens?.small).toBeDefined();
      expect(config.policy?.selection?.preference).toHaveLength(1);
    });

    it("throws field-level error for invalid policy block", () => {
      expect(() =>
        parseConfig({
          solo: {
            transport: {
              type: "stdio",
              command: "solo",
            },
          },
          policy: {
            selection: {
              preference: [{}], // Missing both tool_type and tool_name
            },
          },
        }),
      ).toThrow(/policy|selection|preference/);
    });

    it("accepts policy with only tool_type in preference selector", () => {
      const config = parseConfig({
        solo: {
          transport: {
            type: "stdio",
            command: "solo",
          },
        },
        policy: {
          selection: {
            preference: [
              {
                tool_type: "agent-type",
              },
            ],
          },
        },
      });

      expect(config.policy?.selection?.preference?.[0]?.tool_type).toBe(
        "agent-type",
      );
    });

    it("accepts policy with only tool_name in preference selector", () => {
      const config = parseConfig({
        solo: {
          transport: {
            type: "stdio",
            command: "solo",
          },
        },
        policy: {
          selection: {
            preference: [
              {
                tool_name: "my-agent",
              },
            ],
          },
        },
      });

      expect(config.policy?.selection?.preference?.[0]?.tool_name).toBe(
        "my-agent",
      );
    });

    it("accepts policy with both tool_type and tool_name in preference selector", () => {
      const config = parseConfig({
        solo: {
          transport: {
            type: "stdio",
            command: "solo",
          },
        },
        policy: {
          selection: {
            preference: [
              {
                tool_type: "agent-type",
                tool_name: "my-agent",
              },
            ],
          },
        },
      });

      expect(config.policy?.selection?.preference?.[0]).toEqual({
        tool_type: "agent-type",
        tool_name: "my-agent",
      });
    });
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
