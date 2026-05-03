import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { parseConfig, type SoloConfig } from "./config.js";
import { StdioTransport } from "./transport/stdio.js";
import { SoloClient } from "./solo-client.js";
import { createLogger, type Logger } from "./logger.js";
import { buildClassifierPolicy, defaultPolicy } from "./classifier.js";
import { listAgentTiers, ListAgentTiersInputSchema } from "./tools/list-agent-tiers.js";
import {
  resolveAgentToolHandler,
  ResolveAgentToolInputSchema,
  type ResolveAgentToolInput,
} from "./tools/resolve-agent-tool.js";
import {
  spawnAgentHandler,
  SpawnAgentInputSchema,
  type SpawnAgentInput,
} from "./tools/spawn-agent.js";

export interface MCPServer {
  start(): Promise<void>;
}

export class ServerConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ServerConfigError";
  }
}

export class DuoServer implements MCPServer {
  private readonly _config: SoloConfig;
  private readonly _mcpServer: McpServer;
  private readonly _soloClient: SoloClient | null;
  private readonly _logger: Logger;

  constructor(config: SoloConfig, soloClient?: SoloClient, logger?: Logger) {
    this._config = config;
    this._mcpServer = new McpServer({ name: "duo", version: "0.1.0" });
    this._soloClient = soloClient || null;
    this._logger = logger || createLogger();
  }

  async start(): Promise<void> {
    const transport = new StdioTransport(this._config.solo.transport);
    const soloClient = this._soloClient || new SoloClient(transport);
    if (!this._soloClient) {
      await soloClient.connect();
    }

    // Build classifier policy from config
    const classifierPolicy = this._config.policy
      ? buildClassifierPolicy(this._config.policy)
      : defaultPolicy();

    const selectionPreference = this._config.policy?.selection?.preference;

    // Register tools
    this._mcpServer.registerTool(
      "list_agent_tiers",
      {
        description:
          "List available agent tools grouped by tier (small, medium, large) with default selection and alternatives",
        inputSchema: ListAgentTiersInputSchema,
      },
      (async () => listAgentTiers(soloClient)) as any,
    );

    this._mcpServer.registerTool(
      "resolve_agent_tool",
      {
        description:
          "Resolve and select an agent tool for a specific tier (small, medium, or large)",
        inputSchema: ResolveAgentToolInputSchema,
      },
      (async (input: unknown) => resolveAgentToolHandler(
        soloClient,
        this._logger,
        input as ResolveAgentToolInput,
        classifierPolicy,
        selectionPreference,
      )) as any,
    );

    this._mcpServer.registerTool(
      "spawn_agent",
      {
        description:
          "Spawn a new agent process for a given tier with optional name and project scope",
        inputSchema: SpawnAgentInputSchema,
      },
      (async (input: unknown) => spawnAgentHandler(
        soloClient,
        this._logger,
        input as SpawnAgentInput,
        classifierPolicy,
        selectionPreference,
      )) as any,
    );

    const serverTransport = new StdioServerTransport();
    await this._mcpServer.connect(serverTransport);
  }

  async stop(): Promise<void> {
    await this._mcpServer.close();
  }
}

export async function createServer(
  rawConfig: unknown,
  logger?: Logger,
): Promise<DuoServer> {
  let config: SoloConfig;
  try {
    config = parseConfig(rawConfig);
  } catch (err) {
    throw new ServerConfigError(err instanceof Error ? err.message : String(err));
  }
  return new DuoServer(config, undefined, logger);
}
