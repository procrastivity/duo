import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { parseConfig, type SoloConfig } from "./config.js";
import { StdioTransport } from "./transport/stdio.js";
import { SoloClient } from "./solo-client.js";
import { listAgentTiers, ListAgentTiersInputSchema } from "./tools/list-agent-tiers.js";
import {
  resolveAgentToolHandler,
  ResolveAgentToolInputSchema,
} from "./tools/resolve-agent-tool.js";

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

  constructor(config: SoloConfig, soloClient?: SoloClient) {
    this._config = config;
    this._mcpServer = new McpServer({ name: "duo", version: "0.1.0" });
    this._soloClient = soloClient || null;
  }

  async start(): Promise<void> {
    const transport = new StdioTransport(this._config.solo.transport);
    const soloClient = this._soloClient || new SoloClient(transport);
    if (!this._soloClient) {
      await soloClient.connect();
    }

    // Register tools
    this._mcpServer.registerTool(
      "list_agent_tiers",
      {
        description:
          "List available agent tools grouped by tier (small, medium, large) with default selection and alternatives",
        inputSchema: ListAgentTiersInputSchema,
      },
      async () => listAgentTiers(soloClient),
    );

    this._mcpServer.registerTool(
      "resolve_agent_tool",
      {
        description:
          "Resolve and select an agent tool for a specific tier (small, medium, or large)",
        inputSchema: ResolveAgentToolInputSchema,
      },
      async (input) => resolveAgentToolHandler(soloClient, input),
    );

    const serverTransport = new StdioServerTransport();
    await this._mcpServer.connect(serverTransport);
  }

  async stop(): Promise<void> {
    await this._mcpServer.close();
  }
}

export async function createServer(rawConfig: unknown): Promise<DuoServer> {
  let config: SoloConfig;
  try {
    config = parseConfig(rawConfig);
  } catch (err) {
    throw new ServerConfigError(err instanceof Error ? err.message : String(err));
  }
  return new DuoServer(config);
}
