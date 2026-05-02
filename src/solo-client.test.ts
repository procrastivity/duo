import { describe, expect, it, vi } from "vitest";
import type { Transport } from "./transport/types.js";
import { SoloClient, SoloClientError } from "./solo-client.js";

function createMockTransport() {
  const transport: Transport & { simulateMessage: (msg: unknown) => void } = {
    onmessage: undefined,
    onerror: undefined,
    onclose: undefined,
    start: vi.fn().mockResolvedValue(undefined),
    send: vi.fn().mockResolvedValue(undefined),
    close: vi.fn().mockResolvedValue(undefined),
    simulateMessage(msg: unknown) {
      transport.onmessage?.(msg);
    },
  };
  return transport;
}

describe("SoloClient", () => {
  it("a test double can satisfy the MCPClient interface", () => {
    const transport = createMockTransport();
    const client = new SoloClient(transport);
    expect(client).toBeInstanceOf(SoloClient);
    expect(typeof client.connect).toBe("function");
    expect(typeof client.disconnect).toBe("function");
    expect(typeof client.listAgentTools).toBe("function");
  });

  it("connect calls transport.start()", async () => {
    const transport = createMockTransport();
    const client = new SoloClient(transport);
    await client.connect();
    expect(transport.start).toHaveBeenCalledOnce();
  });

  it("listAgentTools returns parsed tools when mock transport returns valid payload", async () => {
    const transport = createMockTransport();
    const client = new SoloClient(transport);
    await client.connect();

    const tools = [
      { name: "tool_a", description: "Tool A" },
      { name: "tool_b", description: "Tool B" },
    ];

    transport.send = vi.fn().mockImplementation(async (message) => {
      const msg = message as { id: number };
      transport.simulateMessage({
        jsonrpc: "2.0",
        id: msg.id,
        result: { tools },
      });
    });

    const result = await client.listAgentTools();
    expect(result).toEqual(tools);
  });

  it("listAgentTools throws structured error when mock transport returns an error", async () => {
    const transport = createMockTransport();
    const client = new SoloClient(transport);
    await client.connect();

    transport.send = vi.fn().mockImplementation(async (message) => {
      const msg = message as { id: number };
      transport.simulateMessage({
        jsonrpc: "2.0",
        id: msg.id,
        error: { code: -32000, message: "Server error" },
      });
    });

    await expect(client.listAgentTools()).rejects.toThrow(SoloClientError);
    await expect(client.listAgentTools()).rejects.toThrow(
      "MCP error -32000: Server error",
    );
  });
});
