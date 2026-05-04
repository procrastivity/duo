import { StdioTransport } from "../transport/stdio.js";
import { resolveTransportCommand } from "../transport/resolve-command.js";
import { SoloClient, SoloClientError } from "../solo-client.js";
import { loadConfig, type LoadedConfig } from "./config-loader.js";
import { writeErr } from "./output.js";

export interface ConnectOptions {
  cwd?: string;
  quiet?: boolean;
}

export interface ConnectedSolo {
  client: SoloClient;
  config: LoadedConfig;
  dispose: () => Promise<void>;
}

export const EXIT_OK = 0;
export const EXIT_USER_ERROR = 1;
export const EXIT_SOLO_ERROR = 2;
export const EXIT_CONNECT_ERROR = 3;

export const connectSolo = async (opts: ConnectOptions = {}): Promise<ConnectedSolo> => {
  const cwd = opts.cwd ?? process.cwd();
  let loaded: LoadedConfig;
  try {
    loaded = loadConfig({ cwd });
  } catch (err) {
    writeErr(err instanceof Error ? err.message : String(err));
    process.exit(EXIT_USER_ERROR);
  }

  let command: string;
  try {
    command = resolveTransportCommand(loaded.config.solo.transport.command);
  } catch (err) {
    writeErr(err instanceof Error ? err.message : String(err));
    process.exit(EXIT_USER_ERROR);
  }

  const transport = new StdioTransport({ ...loaded.config.solo.transport, command });
  const logger = opts.quiet
    ? undefined
    : {
        info: (msg: string, fields?: Record<string, unknown>) => {
          writeErr(`[info] ${msg}${fields ? " " + JSON.stringify(fields) : ""}`);
        },
        warn: (msg: string, fields?: Record<string, unknown>) => {
          writeErr(`[warn] ${msg}${fields ? " " + JSON.stringify(fields) : ""}`);
        },
      };

  const client = new SoloClient(transport, { cwd, env: process.env, logger });

  try {
    await client.connect();
  } catch (err) {
    writeErr(
      `Failed to connect to Solo: ${err instanceof Error ? err.message : String(err)}`,
    );
    process.exit(EXIT_CONNECT_ERROR);
  }

  return {
    client,
    config: loaded,
    dispose: async () => {
      try {
        await client.disconnect();
      } catch {
        // ignore close errors
      }
    },
  };
};

export const handleSoloError = (err: unknown): never => {
  if (err instanceof SoloClientError) {
    writeErr(JSON.stringify({ code: err.code, message: err.message }));
    process.exit(EXIT_SOLO_ERROR);
  }
  writeErr(err instanceof Error ? err.message : String(err));
  process.exit(EXIT_USER_ERROR);
};
