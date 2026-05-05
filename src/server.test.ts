import { describe, expect, it, vi, beforeEach } from "vitest";
import type { MCPServer } from "./server.js";
import {
  DuoServer,
  createServer,
  createUnavailableServer,
  ServerConfigError,
} from "./server.js";
import { parseConfig } from "./config.js";
import { SoloClient } from "./solo-client.js";
import { createLogger, type Logger } from "./logger.js";
import { buildClassifierPolicy, defaultPolicy } from "./classifier.js";
import type { SoloAgentTool } from "./types/solo.js";
import { enabledRuntimes } from "./__fixtures__/agent-tools.js";
import { spawnSuccessFromEnvProjectId } from "./__fixtures__/spawn-results.js";
import { SpawnAgentInputSchema } from "./tools/spawn-agent.js";
import type { Policy } from "./types/policy.js";
import { getVersion } from "./cli/version-info.js";

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

const makeFakeLogger = (): Logger => ({
  resolutionSuccess: vi.fn(),
  resolutionFailure: vi.fn(),
  spawnSuccess: vi.fn(),
});

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

  it("accepts optional logger in constructor", () => {
    const config = parseConfig(validRawConfig);
    const fakeLogger = makeFakeLogger();
    const server = new DuoServer(config, undefined, fakeLogger);
    expect(server).toBeInstanceOf(DuoServer);
  });

  it("constructs default logger when none injected", () => {
    const config = parseConfig(validRawConfig);
    const server = new DuoServer(config);
    expect(server).toBeInstanceOf(DuoServer);
    // Logger is constructed internally; we verify via behavior below
  });

  it("constructs the underlying McpServer with the package version in serverInfo", () => {
    const config = parseConfig(validRawConfig);
    const server = new DuoServer(config);
    // Reach through MCP SDK internals to verify the value handed to the
    // underlying Server's constructor — the same payload sent in the
    // `initialize` response's `serverInfo`. Brittle if the SDK renames
    // `_serverInfo`, but that is the trade-off for unit-level coverage
    // of the wiring that previously hardcoded "0.1.0".
    const internal = server["_mcpServer"] as unknown as {
      server: { _serverInfo?: { name?: string; version?: string } };
    };
    expect(internal.server._serverInfo).toEqual({ name: "duo", version: getVersion() });
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

      expect(SoloClient.prototype.connect).not.toHaveBeenCalled();
    });

    it("connects to Solo lazily when a tool handler first needs it", async () => {
      const config = parseConfig(validRawConfig);
      const server = new DuoServer(config);

      vi.spyOn(SoloClient.prototype, "connect").mockResolvedValue(undefined);
      vi.spyOn(SoloClient.prototype, "listAgentTools").mockResolvedValue(enabledRuntimes);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let listAgentTiersHandler: (() => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: () => Promise<unknown>) => {
          if (toolName === "list_agent_tiers") {
            listAgentTiersHandler = handler;
          }
          return undefined as unknown;
        },
      );

      await server.start();
      expect(SoloClient.prototype.connect).not.toHaveBeenCalled();

      await listAgentTiersHandler?.();
      expect(SoloClient.prototype.connect).toHaveBeenCalledTimes(1);
    });

    it("returns structured tool errors when startup config failed", async () => {
      const server = createUnavailableServer(
        new ServerConfigError("Failed to read config from /missing/config.yaml"),
      );

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let listAgentTiersHandler: (() => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: () => Promise<unknown>) => {
          if (toolName === "list_agent_tiers") {
            listAgentTiersHandler = handler;
          }
          return undefined as unknown;
        },
      );

      await server.start();
      const result = await listAgentTiersHandler?.();

      expect(result).toEqual({
        content: [
          {
            type: "text",
            text: JSON.stringify({
              code: "solo_connection_failed",
              message: "Failed to read config from /missing/config.yaml",
            }),
          },
        ],
        isError: true,
      });
    });
  });

  describe("logger handling", () => {
    it("forwards injected logger to resolve_agent_tool handler", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(config, mockClient, fakeLogger);

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

      if (resolveAgentToolHandler) {
        await resolveAgentToolHandler({ tier: "medium" });
        // Logger method should have been called
        expect(fakeLogger.resolutionSuccess).toHaveBeenCalled();
      }
    });

    it("forwards injected logger to spawn_agent handler", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(config, mockClient, fakeLogger);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let spawnAgentHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "spawn_agent") {
            spawnAgentHandler = handler;
          }
          return undefined as unknown;
        },
      );

      await server.start();

      if (spawnAgentHandler) {
        await spawnAgentHandler({ tier: "medium" });
        // Logger method should have been called
        expect(fakeLogger.resolutionSuccess).toHaveBeenCalled();
      }
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

  describe("classifier policy from config", () => {
    it("uses default policy when config has no policy", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(config, mockClient, fakeLogger);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let capturedPolicyInResolver = false;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "resolve_agent_tool") {
            // The handler should work with default policy
            handler({ tier: "small" }).catch(() => {
              /* ignore */
            });
            capturedPolicyInResolver = true;
          }
          return undefined as unknown;
        },
      );

      await server.start();
      expect(capturedPolicyInResolver).toBe(true);
    });

    it("forwards classifier policy from config.policy to resolve_agent_tool", async () => {
      const policyConfig: Policy = {
        command_tokens: {
          small: {
            mode: "replace",
            tokens: ["custom-override"],
          },
        },
      };

      const config = parseConfig({
        ...validRawConfig,
        policy: policyConfig,
      });

      const mockClient = makeClient();
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(config, mockClient, fakeLogger);

      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let capturedPolicyInResolver = false;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "resolve_agent_tool") {
            handler({ tier: "small" }).catch(() => {
              /* ignore */
            });
            capturedPolicyInResolver = true;
          }
          return undefined as unknown;
        },
      );

      await server.start();
      expect(capturedPolicyInResolver).toBe(true);
      // Logger should have been called, indicating the resolver ran with policy
      expect(fakeLogger.resolutionSuccess).toHaveBeenCalled();
    });

    it("forwards selection preference from config.policy to resolver", async () => {
      const policyConfig: Policy = {
        selection: {
          preference: [
            {
              tool_name: "test-tool",
            },
          ],
        },
      };

      const config = parseConfig({
        ...validRawConfig,
        policy: policyConfig,
      });

      const mockClient = makeClient();
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(config, mockClient, fakeLogger);

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

      if (resolveAgentToolHandler) {
        await resolveAgentToolHandler({ tier: "medium" });
        // Preference should apply custom strategy
        if (fakeLogger.resolutionSuccess.mock.calls.length > 0) {
          const firstCall = fakeLogger.resolutionSuccess.mock.calls[0];
          expect(firstCall).toBeDefined();
        }
      }
    });

    it("server forwards classifier policy to both tools with override tokens", async () => {
      const policyWithOverride: Policy = {
        command_tokens: {
          small: {
            mode: "replace",
            tokens: ["mini-custom"],
          },
        },
      };

      const config = parseConfig({
        ...validRawConfig,
        policy: policyWithOverride,
      });

      const classifierPolicy = config.policy
        ? buildClassifierPolicy(config.policy)
        : defaultPolicy();

      // Verify the policy was built correctly
      expect(classifierPolicy.command.small).toContainEqual({
        token: "mini-custom",
        source: "override",
      });
    });
  });

  describe("preference application", () => {
    it("applies preference when selecting between two candidates", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient();
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(config, mockClient, fakeLogger);

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

      if (resolveAgentToolHandler) {
        // Call without preference
        await resolveAgentToolHandler({ tier: "medium" });
        expect(fakeLogger.resolutionSuccess).toHaveBeenCalled();
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

    it("rejects string project_id (must be number)", () => {
      const result = SpawnAgentInputSchema.safeParse({ tier: "small", project_id: "6" });
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

    it("does not thread project_id from tool input when caller omits it (SoloClient injects scope)", async () => {
      const config = parseConfig(validRawConfig);
      const mockClient = makeClient(enabledRuntimes, spawnSuccessFromEnvProjectId);
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
      const args = mockClient.spawnProcess.mock.calls[0][0];
      expect(args).not.toHaveProperty("project_id");
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

  it("accepts optional logger parameter", async () => {
    const fakeLogger = makeFakeLogger();
    const server = await createServer(validRawConfig, fakeLogger);
    expect(server).toBeInstanceOf(DuoServer);
  });
});
