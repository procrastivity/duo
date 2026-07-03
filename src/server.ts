import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { parseConfig, type SoloConfig } from "./config.js";
import { StdioTransport } from "./transport/stdio.js";
import { resolveTransportCommand } from "./transport/resolve-command.js";
import { SoloClient } from "./solo-client.js";
import { createLogger, type Logger } from "./logger.js";
import { getVersion } from "./cli/version-info.js";
import { listPresets, ListPresetsInputSchema } from "./tools/list-presets.js";
import {
  resolvePresetHandler,
  ResolvePresetInputSchema,
  type ResolvePresetInput,
} from "./tools/resolve-preset.js";
import {
  launchAgentHandler,
  LaunchAgentInputSchema,
  type LaunchAgentInput,
} from "./tools/launch-agent.js";
import {
  listProvidersHandler,
  ListProvidersInputSchema,
} from "./tools/list-providers.js";
import {
  setProviderEnabledHandler,
  SetProviderEnabledInputSchema,
  type SetProviderEnabledInput,
} from "./tools/set-provider-enabled.js";

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

  /**
   * Preset resolution is config- + provider-state driven and needs no Solo
   * connection, but a failed startup config must still surface as a structured
   * tool error. Returns a `ToolResult` to short-circuit, or `null` to proceed.
   */
  private _startupToolError(): ToolResult | null {
    if (this._startupError) {
      return toolError(
        "solo_connection_failed",
        startupErrorMessage(this._startupError),
      );
    }
    return null;
  }

  async start(): Promise<void> {
    const presets = this._config?.presets;

    // Register tools. Public tool names are kept per step-03/D1; only the
    // behavior and result shapes are preset-driven now.
    this._mcpServer.registerTool(
      "list_presets",
      {
        description:
          "List the configured agent presets with per-preset availability and definitions",
        inputSchema: ListPresetsInputSchema,
      },
      (async () =>
        this._startupToolError() ?? listPresets(presets)) as any,
    );

    this._mcpServer.registerTool(
      "resolve_preset",
      {
        description:
          "Resolve and select the agent tool for a configured preset (optionally avoiding a provider)",
        inputSchema: ResolvePresetInputSchema,
      },
      (async (input: unknown) =>
        this._startupToolError() ??
        resolvePresetHandler(
          this._logger,
          input as ResolvePresetInput,
          presets,
        )) as any,
    );

    this._mcpServer.registerTool(
      "launch_agent",
      {
        description:
          "Launch a new agent process for a configured preset with optional name, project scope, provider avoidance, and caller extra_args",
        inputSchema: LaunchAgentInputSchema,
      },
      (async (input: unknown) =>
        this._withSoloClient((soloClient) => launchAgentHandler(
          soloClient,
          this._logger,
          input as LaunchAgentInput,
          presets,
        ))) as any,
    );

    // Provider-state tools are offline (D6): they read/write only the XDG
    // provider state, so they register through the `_startupToolError()` guard
    // (like list_presets/resolve_preset), never `_withSoloClient`.
    this._mcpServer.registerTool(
      "list_providers",
      {
        description:
          "List the providers tracked in provider state with their enabled/disabled status",
        inputSchema: ListProvidersInputSchema,
      },
      (async () =>
        this._startupToolError() ?? listProvidersHandler()) as any,
    );

    this._mcpServer.registerTool(
      "set_provider_enabled",
      {
        description:
          "Enable or disable a provider in provider state (offline; validates the provider label)",
        inputSchema: SetProviderEnabledInputSchema,
      },
      (async (input: unknown) =>
        this._startupToolError() ??
        setProviderEnabledHandler(input as SetProviderEnabledInput)) as any,
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
 * Boot the Duo MCP server. Loads config + presets, constructs the server,
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
      ...(loaded.config.presets !== undefined && {
        presets: loaded.config.presets,
      }),
    };
    server = await createServer(rawConfig, logger);
  } catch (err) {
    server = createUnavailableServer(err, logger);
  }
  await server.start();
}
