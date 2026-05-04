import type { ChildProcess } from "node:child_process";
import { execa } from "execa";
import type { SoloConfig } from "../config.js";
import type { Transport } from "./types.js";

type StdioConfigBase = SoloConfig["solo"]["transport"];
/** Transport config with a resolved (non-optional) command string. */
export type StdioConfig = Omit<StdioConfigBase, "command"> & { command: string };

export class StdioTransport implements Transport {
  onmessage?: (message: unknown) => void;
  onerror?: (error: Error) => void;
  onclose?: () => void;

  private readonly _config: StdioConfig;
  private _process?: ChildProcess;
  private _buffer = "";

  constructor(config: StdioConfig) {
    this._config = config;
  }

  async start(): Promise<void> {
    const { command, args, cwd, env } = this._config;

    const subprocess = execa(command, args, {
      stdin: "pipe",
      stdout: "pipe",
      stderr: "inherit",
      cwd,
      env: env ? { ...process.env, ...env } : undefined,
    });

    // execa subprocesses double as a promise that rejects on non-zero exit
    // or signal termination. We intentionally kill the child on close(), so
    // swallow that rejection — surfaces via onerror/onclose instead.
    void (subprocess as unknown as Promise<unknown>).catch(() => {});

    this._process = subprocess as unknown as ChildProcess;

    subprocess.stdout?.on("data", (chunk: Buffer | string) => {
      this._buffer += chunk.toString();
      const lines = this._buffer.split("\n");
      this._buffer = lines.pop() ?? "";

      for (const line of lines) {
        if (!line.trim()) continue;
        try {
          this.onmessage?.(JSON.parse(line));
        } catch (err) {
          this.onerror?.(err instanceof Error ? err : new Error(String(err)));
        }
      }
    });

    subprocess.on("error", (err: Error) => {
      this.onerror?.(err);
    });

    subprocess.on("close", () => {
      this.onclose?.();
    });
  }

  async send(message: unknown): Promise<void> {
    if (!this._process?.stdin) {
      throw new Error("Transport not started");
    }
    await new Promise<void>((resolve, reject) => {
      this._process!.stdin!.write(
        JSON.stringify(message) + "\n",
        (err) => (err ? reject(err) : resolve()),
      );
    });
  }

  async close(): Promise<void> {
    this._process?.kill();
    this._process = undefined;
  }
}
