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

  it("throws field-level error for missing required field", () => {
    expect(() =>
      parseConfig({
        solo: {
          transport: {
            type: "stdio",
          },
        },
      }),
    ).toThrow("solo.transport.command");
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
});
