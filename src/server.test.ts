import { describe, expect, it, vi, beforeEach } from "vitest";

// Mocked before any imports of `server.js` so DuoServer's `new McpServer(...)`
// goes through a spy. Re-exports the real class so behavior is preserved.
vi.mock("@modelcontextprotocol/sdk/server/mcp.js", async () => {
  const actual =
    await vi.importActual<typeof import("@modelcontextprotocol/sdk/server/mcp.js")>(
      "@modelcontextprotocol/sdk/server/mcp.js",
    );
  const McpServerSpy = vi.fn(function (...args: ConstructorParameters<typeof actual.McpServer>) {
    return new actual.McpServer(...args);
  }) as unknown as typeof actual.McpServer;
  return { ...actual, McpServer: McpServerSpy };
});

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { MCPServer } from "./server.js";
import {
  DuoServer,
  createServer,
  createUnavailableServer,
  ServerConfigError,
} from "./server.js";
import { parseConfig } from "./config.js";
import { SoloClient } from "./solo-client.js";
import { type Logger } from "./logger.js";
import { spawnSuccessFromEnvProjectId } from "./__fixtures__/spawn-results.js";
import { LaunchAgentInputSchema } from "./tools/launch-agent.js";
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

// Config carrying presets so the resolve/spawn handlers can succeed. The defs
// are provider-less, so they are always eligible (no filesystem state needed).
const rawConfigWithPresets = {
  ...validRawConfig,
  presets: {
    small: [{ id: "s", agent_tool_id: 1 }],
    medium: [{ id: "m", agent_tool_id: 2 }],
  },
};

const makeClient = (spawnResult: unknown = spawnSuccessFromEnvProjectId) =>
  ({
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
    const double: MCPServer = { start: async () => {} };
    expect(double).toBeDefined();
    expect(typeof double.start).toBe("function");
  });
});

describe("DuoServer", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("instantiates with valid config", () => {
    const server = new DuoServer(parseConfig(validRawConfig));
    expect(server).toBeInstanceOf(DuoServer);
  });

  it("accepts optional soloClient in constructor", () => {
    const server = new DuoServer(parseConfig(validRawConfig), makeClient());
    expect(server).toBeInstanceOf(DuoServer);
  });

  it("accepts optional logger in constructor", () => {
    const server = new DuoServer(parseConfig(validRawConfig), undefined, makeFakeLogger());
    expect(server).toBeInstanceOf(DuoServer);
  });

  it("constructs the underlying McpServer with the package version in serverInfo", () => {
    (McpServer as unknown as ReturnType<typeof vi.fn>).mockClear();
    new DuoServer(parseConfig(validRawConfig));
    expect(McpServer).toHaveBeenCalledWith({ name: "duo", version: getVersion() });
  });

  describe("tool registration", () => {
    it("registers exactly the five public tool names", async () => {
      const server = new DuoServer(parseConfig(validRawConfig), makeClient());
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      const names: string[] = [];
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string) => {
          names.push(toolName);
          return undefined as unknown;
        },
      );

      await server.start();
      expect(names.slice().sort()).toEqual([
        "launch_agent",
        "list_presets",
        "list_providers",
        "resolve_preset",
        "set_provider_enabled",
      ]);
    });
  });

  describe("client injection", () => {
    it("creates new SoloClient lazily — none constructed at start()", async () => {
      const server = new DuoServer(parseConfig(validRawConfig));
      vi.spyOn(SoloClient.prototype, "connect").mockResolvedValue(undefined);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      await expect(server.start()).resolves.not.toThrow();
      expect(SoloClient.prototype.connect).not.toHaveBeenCalled();
    });

    it("connects to Solo lazily when the spawn handler first needs it", async () => {
      const server = new DuoServer(parseConfig(rawConfigWithPresets));

      vi.spyOn(SoloClient.prototype, "connect").mockResolvedValue(undefined);
      vi.spyOn(SoloClient.prototype, "spawnProcess").mockResolvedValue(
        spawnSuccessFromEnvProjectId as never,
      );
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let spawnHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "launch_agent") spawnHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();
      expect(SoloClient.prototype.connect).not.toHaveBeenCalled();

      await spawnHandler?.({ preset: "medium" });
      expect(SoloClient.prototype.connect).toHaveBeenCalledTimes(1);
    });

    it("list_presets needs no Solo connection", async () => {
      const server = new DuoServer(parseConfig(rawConfigWithPresets));
      vi.spyOn(SoloClient.prototype, "connect").mockResolvedValue(undefined);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let listHandler: (() => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: () => Promise<unknown>) => {
          if (toolName === "list_presets") listHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();
      const result = (await listHandler?.()) as Record<string, unknown>;
      expect(SoloClient.prototype.connect).not.toHaveBeenCalled();
      expect(Object.keys(result).sort()).toEqual(["medium", "small"]);
    });

    it("returns structured tool errors when startup config failed", async () => {
      const server = createUnavailableServer(
        new ServerConfigError("Failed to read config from /missing/config.yaml"),
      );
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let listHandler: (() => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: () => Promise<unknown>) => {
          if (toolName === "list_presets") listHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();
      const result = await listHandler?.();

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

  describe("preset wiring", () => {
    it("forwards config presets to resolve_preset (success logs)", async () => {
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(parseConfig(rawConfigWithPresets), makeClient(), fakeLogger);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let resolveHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "resolve_preset") resolveHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();
      const result = await resolveHandler?.({ preset: "medium" });
      expect((result as { isError?: boolean }).isError).toBeFalsy();
      expect(fakeLogger.resolutionSuccess).toHaveBeenCalled();
    });

    it("resolve_preset returns unknown_preset when config has no matching preset", async () => {
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(parseConfig(validRawConfig), makeClient(), fakeLogger);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let resolveHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "resolve_preset") resolveHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();
      const result = (await resolveHandler?.({ preset: "medium" })) as {
        content: Array<{ text: string }>;
      };
      expect(JSON.parse(result.content[0].text).code).toBe("unknown_preset");
      expect(fakeLogger.resolutionFailure).toHaveBeenCalled();
    });

    it("forwards config presets to launch_agent (spawnProcess called)", async () => {
      const mockClient = makeClient();
      const fakeLogger = makeFakeLogger();
      const server = new DuoServer(parseConfig(rawConfigWithPresets), mockClient, fakeLogger);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let spawnHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "launch_agent") spawnHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();
      await spawnHandler?.({ preset: "medium" });
      expect(mockClient.spawnProcess).toHaveBeenCalled();
      expect(fakeLogger.resolutionSuccess).toHaveBeenCalled();
    });
  });
});

describe("launch_agent tool", () => {
  describe("registration", () => {
    it("registers launch_agent under that exact name", async () => {
      const server = new DuoServer(parseConfig(validRawConfig), makeClient());
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      const registeredToolNames: string[] = [];
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string) => {
          registeredToolNames.push(toolName);
          return undefined as unknown;
        },
      );

      await server.start();
      expect(registeredToolNames).toContain("launch_agent");
    });
  });

  describe("input schema", () => {
    it("rejects missing preset", () => {
      expect(LaunchAgentInputSchema.safeParse({ name: "my-agent" }).success).toBe(false);
    });

    it("rejects empty-string name", () => {
      expect(LaunchAgentInputSchema.safeParse({ preset: "small", name: "" }).success).toBe(false);
    });

    it("rejects string project_id (must be number)", () => {
      expect(LaunchAgentInputSchema.safeParse({ preset: "small", project_id: "6" }).success).toBe(false);
    });
  });

  describe("handler", () => {
    beforeEach(() => {
      vi.clearAllMocks();
    });

    it("calls spawnProcess on the injected SoloClient", async () => {
      const mockClient = makeClient();
      const server = new DuoServer(parseConfig(rawConfigWithPresets), mockClient);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let capturedHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "launch_agent") capturedHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();
      await capturedHandler?.({ preset: "medium" });
      expect(mockClient.spawnProcess).toHaveBeenCalled();
    });

    it("does not thread project_id from tool input when caller omits it", async () => {
      const mockClient = makeClient(spawnSuccessFromEnvProjectId);
      const server = new DuoServer(parseConfig(rawConfigWithPresets), mockClient);
      vi.spyOn(server["_mcpServer"], "connect").mockResolvedValue(undefined);

      let capturedHandler: ((input?: unknown) => Promise<unknown>) | undefined;
      vi.spyOn(server["_mcpServer"], "registerTool").mockImplementation(
        (toolName: string, _config: unknown, handler: (input?: unknown) => Promise<unknown>) => {
          if (toolName === "launch_agent") capturedHandler = handler;
          return undefined as unknown;
        },
      );

      await server.start();
      await capturedHandler?.({ preset: "medium" });
      const args = (mockClient.spawnProcess as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
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
    const server = await createServer(validRawConfig, makeFakeLogger());
    expect(server).toBeInstanceOf(DuoServer);
  });
});
