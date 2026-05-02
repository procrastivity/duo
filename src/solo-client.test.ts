import { describe, expect, it, vi } from "vitest";
import type { Transport } from "./transport/types.js";
import { SoloClient, SoloClientError } from "./solo-client.js";
import type { SoloAgentTool, SoloSpawnResult } from "./types/solo.js";

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

const fiveRuntimes: SoloAgentTool[] = [
  { id: 1, name: "opencode-ghc-haiku", command: "opencode --model haiku", tool_type: "opencode", enabled: true },
  { id: 2, name: "opencode-ghc-sonnet", command: "opencode --model sonnet", tool_type: "opencode", enabled: true },
  { id: 3, name: "codex-fast", command: "codex --mode fast", tool_type: "codex", enabled: true },
  { id: 4, name: "codex-standard", command: "codex --mode standard", tool_type: "codex", enabled: true },
  { id: 5, name: "codex-flagship", command: "codex --mode flagship", tool_type: "codex", enabled: true },
];

function makeToolsCallResponse(transport: ReturnType<typeof createMockTransport>, tools: unknown[]) {
  transport.send = vi.fn().mockImplementation(async (message) => {
    const msg = message as { id: number };
    transport.simulateMessage({
      jsonrpc: "2.0",
      id: msg.id,
      result: { content: [{ type: "text", text: JSON.stringify(tools) }] },
    });
  });
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

  describe("listAgentTools", () => {
    it("calls tools/call with name list_agent_tools and zero arguments", async () => {
      const transport = createMockTransport();
      const client = new SoloClient(transport);
      await client.connect();
      makeToolsCallResponse(transport, []);

      await client.listAgentTools();

      expect(transport.send).toHaveBeenCalledWith(
        expect.objectContaining({
          method: "tools/call",
          params: { name: "list_agent_tools", arguments: {} },
        }),
      );
    });

    it("returns the parsed SoloAgentTool array for all five known runtimes", async () => {
      const transport = createMockTransport();
      const client = new SoloClient(transport);
      await client.connect();
      makeToolsCallResponse(transport, fiveRuntimes);

      const result = await client.listAgentTools();

      expect(result).toEqual(fiveRuntimes);
    });

    it("throws SoloClientError when transport returns an MCP error", async () => {
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

    it("throws a parse error mentioning the missing field when payload omits command", async () => {
      const transport = createMockTransport();
      const client = new SoloClient(transport);
      await client.connect();

      const malformed = [
        { id: 1, name: "bad-tool", tool_type: "opencode", enabled: true },
      ];
      makeToolsCallResponse(transport, malformed);

      await expect(client.listAgentTools()).rejects.toThrow(/command/);
    });
  });

  describe("spawnProcess", () => {
    function makeSpawnResponse(
      transport: ReturnType<typeof createMockTransport>,
      payload: unknown,
    ) {
      transport.send = vi.fn().mockImplementation(async (message) => {
        const msg = message as { id: number };
        transport.simulateMessage({
          jsonrpc: "2.0",
          id: msg.id,
          result: { content: [{ type: "text", text: JSON.stringify(payload) }] },
        });
      });
    }

    const validSpawnResult: SoloSpawnResult = {
      process_id: "proc-abc123",
      name: "my-helper",
      agent_tool_id: 2,
      project_id: "proj-A",
    };

    it("calls tools/call with spawn_process args and returns the parsed result", async () => {
      const transport = createMockTransport();
      const client = new SoloClient(transport);
      await client.connect();
      makeSpawnResponse(transport, validSpawnResult);

      const result = await client.spawnProcess({
        kind: "agent",
        agent_tool_id: 2,
        name: "my-helper",
        project_id: "proj-A",
      });

      expect(transport.send).toHaveBeenCalledWith(
        expect.objectContaining({
          method: "tools/call",
          params: {
            name: "spawn_process",
            arguments: { kind: "agent", agent_tool_id: 2, name: "my-helper", project_id: "proj-A" },
          },
        }),
      );
      expect(result).toEqual(validSpawnResult);
    });

    it("omits project_id key when caller supplies name but not project_id", async () => {
      const transport = createMockTransport();
      const client = new SoloClient(transport);
      await client.connect();
      makeSpawnResponse(transport, { ...validSpawnResult, project_id: "default-proj" });

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2, name: "my-helper" });

      const sentArgs = (transport.send as ReturnType<typeof vi.fn>).mock.calls[0][0] as {
        params: { arguments: Record<string, unknown> };
      };
      expect(sentArgs.params.arguments).toHaveProperty("name", "my-helper");
      expect(sentArgs.params.arguments).not.toHaveProperty("project_id");
    });

    it("omits both name and project_id keys when caller provides neither", async () => {
      const transport = createMockTransport();
      const client = new SoloClient(transport);
      await client.connect();
      makeSpawnResponse(transport, { ...validSpawnResult, name: "agent-1234" });

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2 });

      const sentArgs = (transport.send as ReturnType<typeof vi.fn>).mock.calls[0][0] as {
        params: { arguments: Record<string, unknown> };
      };
      expect(sentArgs.params.arguments).not.toHaveProperty("name");
      expect(sentArgs.params.arguments).not.toHaveProperty("project_id");
    });

    it("throws SoloClientError with Solo's code and message when transport returns an MCP error", async () => {
      const transport = createMockTransport();
      const client = new SoloClient(transport);
      await client.connect();

      transport.send = vi.fn().mockImplementation(async (message) => {
        const msg = message as { id: number };
        transport.simulateMessage({
          jsonrpc: "2.0",
          id: msg.id,
          error: { code: -32602, message: "name 'my-helper' already in use" },
        });
      });

      const err = await client
        .spawnProcess({ kind: "agent", agent_tool_id: 2, name: "my-helper" })
        .catch((e) => e);
      expect(err).toBeInstanceOf(SoloClientError);
      expect(err.code).toBe(-32602);
      expect(err.message).toContain("already in use");
    });

    it("throws a parse error mentioning process_id when payload omits it", async () => {
      const transport = createMockTransport();
      const client = new SoloClient(transport);
      await client.connect();

      makeSpawnResponse(transport, {
        name: "my-helper",
        agent_tool_id: 2,
        project_id: "proj-A",
      });

      await expect(
        client.spawnProcess({ kind: "agent", agent_tool_id: 2 }),
      ).rejects.toThrow(/process_id/);
    });
  });
});
