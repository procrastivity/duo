import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { parseConfig, type SoloConfig } from "./config.js";
import { StdioTransport } from "./transport/stdio.js";
import { SoloClient } from "./solo-client.js";

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

  constructor(config: SoloConfig) {
    this._config = config;
    this._mcpServer = new McpServer({ name: "duo", version: "0.1.0" });
  }

  async start(): Promise<void> {
    const transport = new StdioTransport(this._config.solo.transport);
    const soloClient = new SoloClient(transport);
    await soloClient.connect();

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
