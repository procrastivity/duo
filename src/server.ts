import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { parseConfig, type SoloConfig } from "./config.js";
import { StdioTransport } from "./transport/stdio.js";
import { resolveTransportCommand } from "./transport/resolve-command.js";
import { SoloClient } from "./solo-client.js";
import { createLogger, type Logger } from "./logger.js";
import { buildClassifierPolicy, defaultPolicy } from "./classifier.js";
import { getVersion } from "./cli/version-info.js";
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

interface TextContent {
  type: "text";
  text: string;
}

interface ToolResult {
  content: TextContent[];
  isError?: boolean;
}

export class ServerConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ServerConfigError";
  }
}

const toolError = (
  code: string | number,
  message: string,
  extra?: Record<string, unknown>,
): ToolResult => ({
  content: [{ type: "text", text: JSON.stringify({ code, message, ...extra }) }],
  isError: true,
});

const startupErrorMessage = (err: unknown): string =>
  err instanceof Error ? err.message : String(err);

export class DuoServer implements MCPServer {
  private readonly _config: SoloConfig | null;
  private readonly _mcpServer: McpServer;
  private readonly _soloClient: SoloClient | null;
  private readonly _logger: Logger;
  private readonly _startupError: Error | null;
  private _soloClientPromise?: Promise<SoloClient>;

  constructor(
    config: SoloConfig | null,
    soloClient?: SoloClient,
    logger?: Logger,
    startupError?: Error,
  ) {
    this._config = config;
    this._mcpServer = new McpServer({ name: "duo", version: getVersion() });
    this._soloClient = soloClient || null;
    this._logger = logger || createLogger();
    this._startupError = startupError || null;
  }

  private async _getSoloClient(): Promise<SoloClient> {
    if (this._startupError) {
      throw this._startupError;
    }
    if (this._soloClient) {
      return this._soloClient;
    }
    if (!this._config) {
      throw new ServerConfigError("Duo server started without Solo configuration");
    }
    if (!this._soloClientPromise) {
      const config = this._config;
      this._soloClientPromise = (async () => {
        const transport = new StdioTransport({
          ...config.solo.transport,
          command: resolveTransportCommand(config.solo.transport.command),
        });
        const soloClient = new SoloClient(transport);
        await soloClient.connect();
        return soloClient;
      })();
      this._soloClientPromise.catch(() => {
        this._soloClientPromise = undefined;
      });
    }
    return this._soloClientPromise;
  }

  private async _withSoloClient<T>(
    run: (soloClient: SoloClient) => Promise<T>,
  ): Promise<T | ToolResult> {
    try {
      return await run(await this._getSoloClient());
    } catch (err) {
      return toolError("solo_connection_failed", startupErrorMessage(err));
    }
  }

  async start(): Promise<void> {
    // Build classifier policy from config
    const classifierPolicy = this._config?.policy
      ? buildClassifierPolicy(this._config.policy)
      : defaultPolicy();

    const selectionPreference = this._config?.policy?.selection?.preference;

    // Register tools
    this._mcpServer.registerTool(
      "list_agent_tiers",
      {
        description:
          "List available agent tools grouped by tier (small, medium, large) with default selection and alternatives",
        inputSchema: ListAgentTiersInputSchema,
      },
      (async () =>
        this._withSoloClient((soloClient) => listAgentTiers(soloClient))) as any,
    );

    this._mcpServer.registerTool(
      "resolve_agent_tool",
      {
        description:
          "Resolve and select an agent tool for a specific tier (small, medium, or large)",
        inputSchema: ResolveAgentToolInputSchema,
      },
      (async (input: unknown) =>
        this._withSoloClient((soloClient) => resolveAgentToolHandler(
          soloClient,
          this._logger,
          input as ResolveAgentToolInput,
          classifierPolicy,
          selectionPreference,
        ))) as any,
    );

    this._mcpServer.registerTool(
      "spawn_agent",
      {
        description:
          "Spawn a new agent process for a given tier with optional name and project scope",
        inputSchema: SpawnAgentInputSchema,
      },
      (async (input: unknown) =>
        this._withSoloClient((soloClient) => spawnAgentHandler(
          soloClient,
          this._logger,
          input as SpawnAgentInput,
          classifierPolicy,
          selectionPreference,
        ))) as any,
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

export function createUnavailableServer(
  err: unknown,
  logger?: Logger,
): DuoServer {
  const startupError =
    err instanceof Error ? err : new ServerConfigError(String(err));
  return new DuoServer(null, undefined, logger, startupError);
}

export interface RunServerOptions {
  cwd?: string;
}

/**
 * Boot the Duo MCP server. Loads config + policy, constructs the server,
 * and starts the stdio transport. If config load or server construction
 * fails, falls back to an unavailable server that surfaces the error
 * via structured tool responses rather than throwing — the stdio
 * transport still starts so clients see actionable error payloads.
 */
export async function runServer(opts: RunServerOptions = {}): Promise<void> {
  const cwd = opts.cwd ?? process.cwd();
  const { loadConfig } = await import("./cli/config-loader.js");
  const logger = createLogger();
  let server: DuoServer;
  try {
    const loaded = loadConfig({ cwd });
    const rawConfig: Record<string, unknown> = {
      solo: loaded.config.solo,
      ...(loaded.config.policy !== undefined && { policy: loaded.config.policy }),
    };
    server = await createServer(rawConfig, logger);
  } catch (err) {
    server = createUnavailableServer(err, logger);
  }
  await server.start();
}
