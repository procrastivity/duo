import { describe, expect, it } from "vitest";
import { loadPolicy, Policy } from "./policy";

describe("loadPolicy", () => {
  describe("valid empty policies", () => {
    it("parses undefined as empty policy", () => {
      const policy = loadPolicy(undefined);
      expect(policy).toEqual({});
    });

    it("parses empty object as empty policy", () => {
      const policy = loadPolicy({});
      expect(policy).toEqual({});
    });
  });

  describe("valid extend mode", () => {
    it("parses command_tokens with extend mode", () => {
      const policy = loadPolicy({
        command_tokens: {
          large: {
            mode: "extend",
            tokens: ["pro"],
          },
        },
      });
      expect(policy.command_tokens?.large?.mode).toBe("extend");
      expect(policy.command_tokens?.large?.tokens).toEqual(["pro"]);
    });

    it("defaults to extend mode when omitted", () => {
      const policy = loadPolicy({
        command_tokens: {
          large: {
            tokens: ["pro"],
          },
        },
      });
      expect(policy.command_tokens?.large?.mode).toBe("extend");
      expect(policy.command_tokens?.large?.tokens).toEqual(["pro"]);
    });
  });

  describe("valid replace mode", () => {
    it("parses command_tokens with replace mode", () => {
      const policy = loadPolicy({
        command_tokens: {
          small: {
            mode: "replace",
            tokens: ["tiny"],
          },
        },
      });
      expect(policy.command_tokens?.small?.mode).toBe("replace");
      expect(policy.command_tokens?.small?.tokens).toEqual(["tiny"]);
    });
  });

  describe("valid mixed modes", () => {
    it("parses multiple tiers with different modes", () => {
      const policy = loadPolicy({
        command_tokens: {
          small: {
            mode: "replace",
            tokens: ["tiny"],
          },
          medium: {
            mode: "extend",
            tokens: ["standard"],
          },
          large: {
            tokens: ["pro"], // defaults to extend
          },
        },
      });
      expect(policy.command_tokens?.small?.mode).toBe("replace");
      expect(policy.command_tokens?.medium?.mode).toBe("extend");
      expect(policy.command_tokens?.large?.mode).toBe("extend");
    });
  });

  describe("empty tokens arrays", () => {
    it("allows empty tokens array with extend mode", () => {
      const policy = loadPolicy({
        command_tokens: {
          small: {
            mode: "extend",
            tokens: [],
          },
        },
      });
      expect(policy.command_tokens?.small?.tokens).toEqual([]);
    });

    it("allows empty tokens array with replace mode", () => {
      const policy = loadPolicy({
        command_tokens: {
          large: {
            mode: "replace",
            tokens: [],
          },
        },
      });
      expect(policy.command_tokens?.large?.tokens).toEqual([]);
    });

    it("treats missing tokens as empty array (default)", () => {
      const policy = loadPolicy({
        command_tokens: {
          medium: {
            mode: "extend",
          },
        },
      });
      expect(policy.command_tokens?.medium?.tokens).toEqual([]);
    });
  });

  describe("valid selection preferences", () => {
    it("parses single preference with tool_type", () => {
      const policy = loadPolicy({
        selection: {
          preference: [{ tool_type: "codex" }],
        },
      });
      expect(policy.selection?.preference).toEqual([{ tool_type: "codex" }]);
    });

    it("parses single preference with tool_name", () => {
      const policy = loadPolicy({
        selection: {
          preference: [{ tool_name: "codex-flagship" }],
        },
      });
      expect(policy.selection?.preference).toEqual([
        { tool_name: "codex-flagship" },
      ]);
    });

    it("parses preference with both tool_type and tool_name", () => {
      const policy = loadPolicy({
        selection: {
          preference: [
            { tool_type: "codex", tool_name: "codex-flagship" },
          ],
        },
      });
      expect(policy.selection?.preference).toEqual([
        { tool_type: "codex", tool_name: "codex-flagship" },
      ]);
    });

    it("parses multiple preference selectors", () => {
      const policy = loadPolicy({
        selection: {
          preference: [
            { tool_type: "codex" },
            { tool_name: "opencode" },
            { tool_type: "another", tool_name: "specific" },
          ],
        },
      });
      expect(policy.selection?.preference?.length).toBe(3);
    });
  });

  describe("valid combined policies", () => {
    it("parses command_tokens and selection together", () => {
      const policy = loadPolicy({
        command_tokens: {
          large: {
            mode: "extend",
            tokens: ["pro"],
          },
        },
        selection: {
          preference: [{ tool_type: "codex" }],
        },
      });
      expect(policy.command_tokens?.large?.tokens).toEqual(["pro"]);
      expect(policy.selection?.preference).toEqual([{ tool_type: "codex" }]);
    });
  });

  describe("invalid cases", () => {
    it("rejects unknown tier label in command_tokens", () => {
      expect(() =>
        loadPolicy({
          command_tokens: {
            huge: {
              tokens: ["gigantic"],
            },
          },
        }),
      ).toThrow("command_tokens");
    });

    it("rejects unknown top-level key", () => {
      expect(() =>
        loadPolicy({
          logging: {},
        }),
      ).toThrow("logging");
    });

    it("rejects empty selector", () => {
      expect(() =>
        loadPolicy({
          selection: {
            preference: [{}],
          },
        }),
      ).toThrow("selection.preference.0");
    });

    it("rejects empty token string", () => {
      expect(() =>
        loadPolicy({
          command_tokens: {
            large: {
              tokens: [""],
            },
          },
        }),
      ).toThrow("command_tokens.large.tokens.0");
    });

    it("rejects bad mode value", () => {
      expect(() =>
        loadPolicy({
          command_tokens: {
            medium: {
              mode: "merge",
              tokens: ["standard"],
            },
          },
        }),
      ).toThrow("command_tokens.medium.mode");
    });

    it("rejects empty preference array", () => {
      expect(() =>
        loadPolicy({
          selection: {
            preference: [],
          },
        }),
      ).toThrow("selection.preference");
    });

    it("rejects unknown key in command_tokens tier", () => {
      expect(() =>
        loadPolicy({
          command_tokens: {
            large: {
              mode: "extend",
              tokens: ["pro"],
              unknown_field: "value",
            },
          },
        }),
      ).toThrow("command_tokens.large");
    });

    it("rejects unknown key in preference selector", () => {
      expect(() =>
        loadPolicy({
          selection: {
            preference: [
              {
                tool_type: "codex",
                unknown_field: "value",
              },
            ],
          },
        }),
      ).toThrow("selection.preference.0");
    });

    it("rejects unknown key in selection object", () => {
      expect(() =>
        loadPolicy({
          selection: {
            preference: [{ tool_type: "codex" }],
            unknown_field: "value",
          },
        }),
      ).toThrow("selection");
    });
  });

  describe("field-level error message shape", () => {
    it("formats error message with path and message", () => {
      try {
        loadPolicy({
          command_tokens: {
            medium: {
              mode: "merge",
            },
          },
        });
        expect.fail("should have thrown");
      } catch (error) {
        const message = (error as Error).message;
        // Error format should be "path: message"
        expect(message).toMatch(/^command_tokens\.medium\.mode:/);
        expect(message).toMatch(/extend|replace/);
      }
    });

    it("formats selector error with index in path", () => {
      try {
        loadPolicy({
          selection: {
            preference: [{ tool_type: "codex" }, {}],
          },
        });
        expect.fail("should have thrown");
      } catch (error) {
        const message = (error as Error).message;
        // Should report the index of the empty selector
        expect(message).toMatch(/selection\.preference\.\d+:/);
        expect(message).toContain("selector must specify");
      }
    });

    it("formats empty token string error", () => {
      try {
        loadPolicy({
          command_tokens: {
            small: {
              tokens: ["valid", ""],
            },
          },
        });
        expect.fail("should have thrown");
      } catch (error) {
        const message = (error as Error).message;
        expect(message).toMatch(/command_tokens\.small\.tokens\./);
      }
    });
  });

  describe("type inference", () => {
    it("returns properly typed Policy object", () => {
      const policy = loadPolicy({
        command_tokens: {
          large: {
            mode: "extend",
            tokens: ["pro"],
          },
        },
        selection: {
          preference: [{ tool_type: "codex", tool_name: "flagship" }],
        },
      });

      // These should be accessible without type errors
      const mode = policy.command_tokens?.large?.mode;
      const tokens = policy.command_tokens?.large?.tokens;
      const preference = policy.selection?.preference;

      expect(mode).toBe("extend");
      expect(tokens).toEqual(["pro"]);
      expect(preference).toBeDefined();
    });
  });
});
