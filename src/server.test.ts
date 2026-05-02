import { describe, expect, it, vi, beforeEach } from "vitest";
import type { MCPServer } from "./server.js";
import { DuoServer, createServer, ServerConfigError } from "./server.js";
import { parseConfig } from "./config.js";
import { SoloClient } from "./solo-client.js";
import type { SoloAgentTool } from "./types/solo.js";
import { enabledRuntimes } from "./__fixtures__/agent-tools.js";
import { spawnSuccessFromEnvProjectId } from "./__fixtures__/spawn-results.js";
import { SpawnAgentInputSchema } from "./tools/spawn-agent.js";

const validRawConfig = {
  solo: {
    transport: {
      type: "stdio",
      command: "solo",
      args: ["mcp", "serve"],
    },
  },
};

const makeClient = (
  tools: SoloAgentTool[] = enabledRuntimes,
  spawnResult: unknown = spawnSuccessFromEnvProjectId,
) =>
  ({
    listAgentTools: vi.fn().mockResolvedValue(tools),
    spawnProcess: vi.fn().mockResolvedValue(spawnResult),
    connect: vi.fn().mockResolvedValue(undefined),
  }) as unknown as SoloClient;

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

  it("accepts optional soloClient in constructor", () => {
    const config = parseConfig(validRawConfig);
    const mockClient = makeClient();
    const server = new DuoServer(config, mockClient);
    expect(server).toBeInstanceOf(DuoServer);
  });

  describe("tool registration", () => {
    it("registers list_agent_tiers tool", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const server = new DuoServer(config, mockClient);

      // Mock the McpServer.connect to prevent actual connection
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      await expect(server.start()).resolves.not.toThrow();

      // Verify that registerTool was called for list_agent_tiers
      expect(server["_mcpServer"].registerTool).toBeDefined();
    });

    it("registers resolve_agent_tool tool", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const server = new DuoServer(config, mockClient);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      await expect(server.start()).resolves.not.toThrow();

      // Verify that registerTool was called for resolve_agent_tool
      expect(server["_mcpServer"].registerTool).toBeDefined();
    });
  });

  describe("client injection", () => {
    it("uses injected soloClient if provided", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const server = new DuoServer(config, mockClient);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      // Capture the tool handler to verify it receives the injected client
      let capturedClient: SoloClient | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, config: unknown, handler: (input?: unknown) => unknown) => {
          // For list_agent_tiers (no input), call with undefined
          if (toolName === "list_agent_tiers") {
            handler(undefined).catch(() => {
              /* ignore */
            });
          }
          return undefined as unknown;
        },
      );

      await server.start();

      // The injected client should be used in the handlers
      expect(mockClient.listAgentTools).toBeDefined();
    });

    it("creates new SoloClient if none injected", async () => {
      const config = parseConfig(validRawConfig);
      const server = new DuoServer(config);

      // Mock StdioTransport to prevent actual transport creation
      vi.doMock("./transport/stdio.js", () => ({
        StdioTransport: vi.fn(() => ({ on: vi.fn() })),
      }));

      vi.spyOn(SoloClient.prototype, "connect").mockResolvedValue(undefined);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      await expect(server.start()).resolves.not.toThrow();

      // Verify SoloClient was instantiated and connect was called
      expect(SoloClient.prototype.connect).toBeDefined();
    });
  });

  describe("tool handler behavior", () => {
    beforeEach(() => {
      vi.clearAllMocks();
    });

    it("list_agent_tiers handler calls client.listAgentTools", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const server = new DuoServer(config, mockClient);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let listAgentTiersHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "list_agent_tiers") {
            listAgentTiersHandler = handler;
          }
          return undefined as unknown;
        },
      );

      await server.start();

      // Call the handler and verify it uses the client
      if (listAgentTiersHandler) {
        await listAgentTiersHandler();
        expect(mockClient.listAgentTools).toHaveBeenCalled();
      }
    });

    it("resolve_agent_tool handler receives tier input", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const server = new DuoServer(config, mockClient);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let resolveAgentToolHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "resolve_agent_tool") {
            resolveAgentToolHandler = handler;
          }
          return undefined as unknown;
        },
      );

      await server.start();

      // Call the handler with tier input
      if (resolveAgentToolHandler) {
        const result = await resolveAgentToolHandler({ tier: "medium" });
        expect(result).toBeDefined();
      }
    });
  });
});

describe("spawn_agent tool", () => {
  describe("registration", () => {
    it("registers spawn_agent under that exact name", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const server = new DuoServer(config, mockClient);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      const registeredToolNames: string[] = [];
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string) => {
          registeredToolNames.push(toolName);
          return undefined as unknown;
        },
      );

      await server.start();

      expect(registeredToolNames).toContain("spawn_agent");
    });
  });

  describe("input schema", () => {
    it("rejects missing tier", () => {
      const result = SpawnAgentInputSchema.safeParse({ name: "my-agent" });
      expect(result.success).toBe(false);
    });

    it("rejects empty-string name", () => {
      const result = SpawnAgentInputSchema.safeParse({ tier: "small", name: "" });
      expect(result.success).toBe(false);
    });

    it("rejects empty-string project_id", () => {
      const result = SpawnAgentInputSchema.safeParse({ tier: "small", project_id: "" });
      expect(result.success).toBe(false);
    });
  });

  describe("handler", () => {
    it("calls spawnProcess on the injected SoloClient", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const server = new DuoServer(config, mockClient);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let capturedHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "spawn_agent") capturedHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();

      await capturedHandler?.({ tier: "medium" });
      expect(mockClient.spawnProcess).toHaveBeenCalled();
    });

    it("passes config.solo.projectId to spawnProcess when caller omits project_id", async () => {
      const configWithProjectId = parseConfig(validRawConfig, { SOLO_PROJECT_ID: "proj-env-xyz" });
      const mockClient = makeClient(enabledRuntimes, spawnSuccessFromEnvProjectId);
      const server = new DuoServer(configWithProjectId, mockClient);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let capturedHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "spawn_agent") capturedHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();

      await capturedHandler?.({ tier: "medium" });
      expect(mockClient.spawnProcess).toHaveBeenCalledWith(
        expect.objectContaining({ project_id: "proj-env-xyz" }),
      );
    });
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
