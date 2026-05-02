import { describe, expect, it } from "vitest";
import type { Transport } from "./types.js";
import { StdioTransport } from "./stdio.js";

describe("Transport interface", () => {
  it("a test double can satisfy the Transport interface", () => {
    const double: Transport = {
      start: async () => {},
      send: async () => {},
      close: async () => {},
    };

    expect(double).toBeDefined();
    expect(typeof double.start).toBe("function");
    expect(typeof double.send).toBe("function");
    expect(typeof double.close).toBe("function");
  });
});

describe("StdioTransport", () => {
  it("can be constructed with valid config", () => {
    const transport = new StdioTransport({
      type: "stdio",
      command: "solo",
      args: ["mcp", "serve"],
    });

    expect(transport).toBeInstanceOf(StdioTransport);
  });

  it("exposes optional callback properties", () => {
    const transport = new StdioTransport({
      type: "stdio",
      command: "solo",
      args: [],
    });

    expect(transport.onmessage).toBeUndefined();
    expect(transport.onerror).toBeUndefined();
    expect(transport.onclose).toBeUndefined();

    const handler = () => {};
    transport.onmessage = handler;
    expect(transport.onmessage).toBe(handler);
  });

  it("send throws when called before start", async () => {
    const transport = new StdioTransport({
      type: "stdio",
      command: "solo",
      args: [],
    });

    await expect(transport.send({ method: "ping" })).rejects.toThrow(
      "Transport not started",
    );
  });
});
