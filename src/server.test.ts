import { describe, expect, it } from "vitest";
import type { MCPServer } from "./server.js";
import { DuoServer, createServer, ServerConfigError } from "./server.js";
import { parseConfig } from "./config.js";

const validRawConfig = {
  solo: {
    transport: {
      type: "stdio",
      command: "solo",
      args: ["mcp", "serve"],
    },
  },
};

describe("MCPServer interface", () => {
  it("a test double can satisfy the MCPServer interface", () => {
    const double: MCPServer = {
      start: async () => {},
    };
    expect(double).toBeDefined();
    expect(typeof double.start).toBe("function");
  });
});

describe("DuoServer", () => {
  it("instantiates with valid config", () => {
    const config = parseConfig(validRawConfig);
    const server = new DuoServer(config);
    expect(server).toBeInstanceOf(DuoServer);
  });
});

describe("createServer", () => {
  it("resolves with a DuoServer for valid config", async () => {
    const server = await createServer(validRawConfig);
    expect(server).toBeInstanceOf(DuoServer);
  });

  it("rejects with ServerConfigError for invalid config", async () => {
    await expect(createServer({})).rejects.toThrow(ServerConfigError);
  });

  it("rejects with structured error message identifying the missing field", async () => {
    await expect(createServer({})).rejects.toThrow(/solo/);
  });
});
