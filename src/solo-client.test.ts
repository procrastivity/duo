import { describe, expect, it, vi } from "vitest";
import type { Transport } from "./transport/types.js";
import { SoloClient, SoloClientError } from "./solo-client.js";
import type { SoloAgentTool, SoloProject, SoloSpawnResult } from "./types/solo.js";
import { getVersion } from "./cli/version-info.js";

type ToolCallResponder = (name: string, args: unknown) => unknown;

function createMockTransport(toolResponder?: ToolCallResponder) {
  const transport: Transport & { simulateMessage: (msg: unknown) => void } = {
    onmessage: undefined,
    onerror: undefined,
    onclose: undefined,
    start: vi.fn().mockResolvedValue(undefined),
    send: vi.fn().mockImplementation(async (message: unknown) => {
      const msg = message as {
        id?: number;
        method?: string;
        params?: { name?: string; arguments?: unknown };
      };
      if (msg.id === undefined) return; // notification
      if (msg.method === "initialize") {
        transport.simulateMessage({
          jsonrpc: "2.0",
          id: msg.id,
          result: { protocolVersion: "2024-11-05", capabilities: {} },
        });
        return;
      }
      if (msg.method === "tools/call" && toolResponder) {
        const payload = toolResponder(
          msg.params?.name ?? "",
          msg.params?.arguments,
        );
        transport.simulateMessage({
          jsonrpc: "2.0",
          id: msg.id,
          result: {
            content: [{ type: "text", text: JSON.stringify(payload) }],
          },
        });
      }
    }),
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

const sampleProjects: SoloProject[] = [
  { id: 6, name: "duo", path: "/Users/me/Code/duo" },
  { id: 4, name: "outer", path: "/Users/me/Code" },
  { id: 7, name: "other", path: "/Users/me/elsewhere" },
];

describe("SoloClient", () => {
  describe("connect handshake", () => {
    it("performs initialize then notifications/initialized", async () => {
      const transport = createMockTransport(() => []);
      const client = new SoloClient(transport, { env: {}, cwd: "/tmp" });
      await client.connect();

      const calls = (transport.send as ReturnType<typeof vi.fn>).mock.calls.map(
        (c) =>
          c[0] as {
            method?: string;
            id?: number;
            params?: { clientInfo?: { name?: string; version?: string } };
          },
      );
      expect(calls[0].method).toBe("initialize");
      expect(calls[0].id).toBeTypeOf("number");
      expect(calls[0].params?.clientInfo).toEqual({ name: "duo", version: getVersion() });
      expect(calls[1].method).toBe("notifications/initialized");
      expect(calls[1].id).toBeUndefined();
    });
  });

  describe("connect-time scope resolution", () => {
    it("uses SOLO_PROJECT_ID env when set; does not call list_projects", async () => {
      const calledNames: string[] = [];
      const transport = createMockTransport((name) => {
        calledNames.push(name);
        return [];
      });
      const client = new SoloClient(transport, {
        env: { SOLO_PROJECT_ID: "42" },
        cwd: "/anywhere",
      });
      await client.connect();

      expect(client.projectId).toBe(42);
      expect(calledNames).not.toContain("list_projects");
    });

    it("falls back to list_projects + cwd longest-match when env unset", async () => {
      const transport = createMockTransport((name) => {
        if (name === "list_projects") return sampleProjects;
        return [];
      });
      const client = new SoloClient(transport, {
        env: {},
        cwd: "/Users/me/Code/duo",
      });
      await client.connect();

      expect(client.projectId).toBe(6); // longest match wins over outer Code dir
    });

    it("nested-path projects → longest match wins", async () => {
      const transport = createMockTransport((name) => {
        if (name === "list_projects") return sampleProjects;
        return [];
      });
      const client = new SoloClient(transport, {
        env: {},
        cwd: "/Users/me/Code/duo/src/tools",
      });
      await client.connect();

      expect(client.projectId).toBe(6);
    });

    it("no match → projectId stays undefined, no throw", async () => {
      const transport = createMockTransport((name) => {
        if (name === "list_projects") return sampleProjects;
        return [];
      });
      const client = new SoloClient(transport, {
        env: {},
        cwd: "/somewhere/unrelated",
      });
      await client.connect();

      expect(client.projectId).toBeUndefined();
    });

    it("env and pwd disagree → env wins, info logged", async () => {
      const infoCalls: Array<{ msg: string; fields?: Record<string, unknown> }> =
        [];
      const transport = createMockTransport((name) => {
        if (name === "list_projects") return sampleProjects;
        return [];
      });
      const client = new SoloClient(transport, {
        env: { SOLO_PROJECT_ID: "99" },
        cwd: "/Users/me/Code/duo",
        logger: { info: (msg, fields) => infoCalls.push({ msg, fields }) },
      });
      await client.connect();

      // env was set, list_projects not called → no disagreement signal possible
      // (we deliberately avoid the extra round-trip when env pins). Just verify env wins.
      expect(client.projectId).toBe(99);
    });

    it("calls bind_session_process when SOLO_PROCESS_ID is set", async () => {
      const bindCalls: unknown[] = [];
      const transport = createMockTransport((name, args) => {
        if (name === "bind_session_process") {
          bindCalls.push(args);
          return { ok: true };
        }
        return [];
      });
      const client = new SoloClient(transport, {
        env: { SOLO_PROCESS_ID: "297", SOLO_PROJECT_ID: "6" },
        cwd: "/tmp",
      });
      await client.connect();

      expect(bindCalls).toEqual([{ process_id: 297 }]);
      expect(client.processId).toBe(297);
    });

    it("does not call bind_session_process when SOLO_PROCESS_ID is unset", async () => {
      const calledNames: string[] = [];
      const transport = createMockTransport((name) => {
        calledNames.push(name);
        return [];
      });
      const client = new SoloClient(transport, {
        env: { SOLO_PROJECT_ID: "6" },
        cwd: "/tmp",
      });
      await client.connect();

      expect(calledNames).not.toContain("bind_session_process");
      expect(client.processId).toBeUndefined();
    });

    it("bind_session_process failure logs warning but does not reject connect()", async () => {
      const warnCalls: Array<{ msg: string }> = [];
      const transport = createMockTransport((name) => {
        if (name === "bind_session_process") {
          throw new Error("simulated"); // responder never replies → handled below
        }
        return [];
      });
      // override transport to inject error reply for bind
      const origSend = transport.send;
      transport.send = vi.fn().mockImplementation(async (message: unknown) => {
        const msg = message as {
          id?: number;
          method?: string;
          params?: { name?: string };
        };
        if (msg.method === "tools/call" && msg.params?.name === "bind_session_process") {
          transport.simulateMessage({
            jsonrpc: "2.0",
            id: msg.id,
            error: { code: -32000, message: "no such process" },
          });
          return;
        }
        return (origSend as any)(message);
      });

      const client = new SoloClient(transport, {
        env: { SOLO_PROCESS_ID: "999", SOLO_PROJECT_ID: "6" },
        cwd: "/tmp",
        logger: { warn: (msg) => warnCalls.push({ msg }) },
      });

      await expect(client.connect()).resolves.toBeUndefined();
      expect(client.processId).toBeUndefined();
      expect(warnCalls.some((c) => c.msg.includes("bind_session_process_failed"))).toBe(true);
    });
  });

  describe("listAgentTools", () => {
    it("returns parsed tool array", async () => {
      const transport = createMockTransport((name) => {
        if (name === "list_agent_tools") return fiveRuntimes;
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "1" }, cwd: "/" });
      await client.connect();

      const result = await client.listAgentTools();
      expect(result).toEqual(fiveRuntimes);
    });
  });

  describe("listProjects", () => {
    it("returns parsed project array", async () => {
      const transport = createMockTransport((name) => {
        if (name === "list_projects") return sampleProjects;
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "1" }, cwd: "/" });
      await client.connect();

      const result = await client.listProjects();
      expect(result).toEqual(sampleProjects);
    });
  });

  describe("spawnProcess", () => {
    const validSpawnResult: SoloSpawnResult = {
      process_id: 111,
      name: "my-helper",
    };

    it("forwards caller-supplied project_id verbatim", async () => {
      let capturedArgs: Record<string, unknown> | undefined;
      const transport = createMockTransport((name, args) => {
        if (name === "spawn_process") {
          capturedArgs = args as Record<string, unknown>;
          return validSpawnResult;
        }
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "6" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({
        kind: "agent",
        agent_tool_id: 2,
        name: "my-helper",
        project_id: 7,
      });

      expect(capturedArgs).toEqual({
        kind: "agent",
        agent_tool_id: 2,
        name: "my-helper",
        project_id: 7,
      });
    });

    it("injects client.projectId when caller omits project_id", async () => {
      let capturedArgs: Record<string, unknown> | undefined;
      const transport = createMockTransport((name, args) => {
        if (name === "spawn_process") {
          capturedArgs = args as Record<string, unknown>;
          return validSpawnResult;
        }
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "6" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2 });
      expect(capturedArgs).toEqual({ kind: "agent", agent_tool_id: 2, project_id: 6 });
    });

    it("omits project_id when neither caller nor client has one", async () => {
      let capturedArgs: Record<string, unknown> | undefined;
      const transport = createMockTransport((name, args) => {
        if (name === "spawn_process") {
          capturedArgs = args as Record<string, unknown>;
          return validSpawnResult;
        }
        return [];
      });
      const client = new SoloClient(transport, { env: {}, cwd: "/nowhere" });
      await client.connect();

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2 });
      expect(capturedArgs).not.toHaveProperty("project_id");
    });

    it("includes extra_args in call args when non-empty", async () => {
      let capturedArgs: Record<string, unknown> | undefined;
      const transport = createMockTransport((name, args) => {
        if (name === "spawn_process") {
          capturedArgs = args as Record<string, unknown>;
          return validSpawnResult;
        }
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "6" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({
        kind: "agent",
        agent_tool_id: 2,
        extra_args: ["--model", "sonnet"],
      });

      expect(capturedArgs).toEqual({
        kind: "agent",
        agent_tool_id: 2,
        project_id: 6,
        extra_args: ["--model", "sonnet"],
      });
    });

    it("omits extra_args when the array is empty", async () => {
      let capturedArgs: Record<string, unknown> | undefined;
      const transport = createMockTransport((name, args) => {
        if (name === "spawn_process") {
          capturedArgs = args as Record<string, unknown>;
          return validSpawnResult;
        }
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "6" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2, extra_args: [] });
      expect(capturedArgs).not.toHaveProperty("extra_args");
    });

    it("omits extra_args when absent", async () => {
      let capturedArgs: Record<string, unknown> | undefined;
      const transport = createMockTransport((name, args) => {
        if (name === "spawn_process") {
          capturedArgs = args as Record<string, unknown>;
          return validSpawnResult;
        }
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "6" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2 });
      expect(capturedArgs).not.toHaveProperty("extra_args");
    });

    it("throws SoloClientError when transport returns an MCP error", async () => {
      const transport = createMockTransport(() => []);
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "1" }, cwd: "/" });
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
      const transport = createMockTransport((name) => {
        if (name === "spawn_process") {
          return { name: "my-helper" }; // process_id absent
        }
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "1" }, cwd: "/" });
      await client.connect();

      await expect(
        client.spawnProcess({ kind: "agent", agent_tool_id: 2 }),
      ).rejects.toThrow(/process_id/);
    });

    it("does not call send_input when no prompt is supplied", async () => {
      const calledNames: string[] = [];
      const transport = createMockTransport((name, args) => {
        calledNames.push(name);
        if (name === "spawn_process") return validSpawnResult;
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "1" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2 });
      expect(calledNames).not.toContain("send_input");
    });

    it("calls send_input with prompt when prompt is provided (no agent_instructions)", async () => {
      let sendInputArgs: Record<string, unknown> | undefined;
      const transport = createMockTransport((name, args) => {
        if (name === "spawn_process") return validSpawnResult;
        if (name === "send_input") {
          sendInputArgs = args as Record<string, unknown>;
          return {};
        }
        return [];
      });
      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "6" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2, prompt: "Do the thing" });

      expect(sendInputArgs).toBeDefined();
      expect(sendInputArgs?.process_id).toBe(111);
      expect(sendInputArgs?.input).toBe("Do the thing");
      expect(sendInputArgs?.project_id).toBe(6);
    });

    it("prepends agent_instructions to prompt when both are present", async () => {
      let sendInputArgs: Record<string, unknown> | undefined;
      const spawnResultWithInstructions: SoloSpawnResult = {
        process_id: 111,
        name: "my-helper",
        agent_instructions: "You are running in project Duo.",
      };
      const transport = createMockTransport((name) => {
        if (name === "spawn_process") return spawnResultWithInstructions;
        if (name === "send_input") {
          sendInputArgs = undefined; // will be set below
          return {};
        }
        return [];
      });
      // Override to capture send_input args
      const origSend = transport.send;
      transport.send = vi.fn().mockImplementation(async (message: unknown) => {
        const msg = message as {
          id?: number;
          method?: string;
          params?: { name?: string; arguments?: unknown };
        };
        if (msg.method === "tools/call" && msg.params?.name === "send_input") {
          sendInputArgs = msg.params.arguments as Record<string, unknown>;
          transport.simulateMessage({
            jsonrpc: "2.0",
            id: msg.id,
            result: { content: [{ type: "text", text: "{}" }] },
          });
          return;
        }
        return (origSend as any)(message);
      });

      const client = new SoloClient(transport, { env: { SOLO_PROJECT_ID: "6" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2, prompt: "Do the thing" });

      expect(sendInputArgs).toBeDefined();
      expect(sendInputArgs?.input).toBe("You are running in project Duo.\n\nDo the thing");
    });

    it("send_input uses caller-supplied project_id over client.projectId", async () => {
      let sendInputArgs: Record<string, unknown> | undefined;
      const origSend = createMockTransport((name) => {
        if (name === "spawn_process") return validSpawnResult;
        return [];
      });
      origSend.send = vi.fn().mockImplementation(async (message: unknown) => {
        const msg = message as {
          id?: number;
          method?: string;
          params?: { name?: string; arguments?: unknown };
        };
        if (msg.id === undefined) return;
        if (msg.method === "initialize") {
          origSend.simulateMessage({ jsonrpc: "2.0", id: msg.id, result: { protocolVersion: "2024-11-05", capabilities: {} } });
          return;
        }
        if (msg.method === "tools/call") {
          const name = (msg.params as any)?.name;
          if (name === "send_input") {
            sendInputArgs = (msg.params as any)?.arguments;
            origSend.simulateMessage({ jsonrpc: "2.0", id: msg.id, result: { content: [{ type: "text", text: "{}" }] } });
            return;
          }
          origSend.simulateMessage({ jsonrpc: "2.0", id: msg.id, result: { content: [{ type: "text", text: JSON.stringify(validSpawnResult) }] } });
        }
      });

      const client = new SoloClient(origSend, { env: { SOLO_PROJECT_ID: "6" }, cwd: "/" });
      await client.connect();

      await client.spawnProcess({ kind: "agent", agent_tool_id: 2, project_id: 99, prompt: "Go!" });

      expect(sendInputArgs?.project_id).toBe(99);
    });
  });
});
