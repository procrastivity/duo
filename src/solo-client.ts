import type { Transport } from "./transport/types.js";
import {
  SoloAgentToolsSchema,
  SoloSpawnResultSchema,
  type SoloAgentTool,
  type SoloSpawnArgs,
  type SoloSpawnResult,
} from "./types/solo.js";

export type { SoloAgentTool, SoloSpawnArgs, SoloSpawnResult };

export class SoloClientError extends Error {
  constructor(
    message: string,
    public readonly code: number,
  ) {
    super(message);
    this.name = "SoloClientError";
  }
}

export class SoloClient {
  private readonly _transport: Transport;
  private _requestId = 0;
  private readonly _pending = new Map<
    number,
    { resolve: (value: unknown) => void; reject: (error: Error) => void }
  >();

  constructor(transport: Transport) {
    this._transport = transport;
  }

  async connect(): Promise<void> {
    this._transport.onmessage = (message) => {
      this._handleMessage(message);
    };
    this._transport.onerror = (error) => {
      for (const pending of this._pending.values()) {
        pending.reject(error);
      }
      this._pending.clear();
    };
    await this._transport.start();
  }

  async disconnect(): Promise<void> {
    await this._transport.close();
  }

  async listAgentTools(): Promise<SoloAgentTool[]> {
    const result = await this._request("tools/call", {
      name: "list_agent_tools",
      arguments: {},
    });
    const response = result as {
      content?: Array<{ type: string; text?: string }>;
    };
    const textContent = response.content?.find((c) => c.type === "text");
    if (!textContent?.text) {
      throw new Error("list_agent_tools returned no text content");
    }
    return SoloAgentToolsSchema.parse(JSON.parse(textContent.text));
  }

  async spawnProcess(args: SoloSpawnArgs): Promise<SoloSpawnResult> {
    const callArgs: Record<string, unknown> = {
      kind: args.kind,
      agent_tool_id: args.agent_tool_id,
      ...(args.name !== undefined && { name: args.name }),
      ...(args.project_id !== undefined && { project_id: args.project_id }),
    };
    const result = await this._request("tools/call", {
      name: "spawn_process",
      arguments: callArgs,
    });
    const response = result as {
      content?: Array<{ type: string; text?: string }>;
    };
    const textContent = response.content?.find((c) => c.type === "text");
    if (!textContent?.text) {
      throw new Error("spawn_process returned no text content");
    }
    return SoloSpawnResultSchema.parse(JSON.parse(textContent.text));
  }

  private _handleMessage(message: unknown): void {
    const msg = message as {
      id?: number;
      result?: unknown;
      error?: { code: number; message: string };
    };

    if (msg.id === undefined) return;

    const pending = this._pending.get(msg.id);
    if (!pending) return;

    this._pending.delete(msg.id);

    if (msg.error) {
      pending.reject(
        new SoloClientError(
          `MCP error ${msg.error.code}: ${msg.error.message}`,
          msg.error.code,
        ),
      );
    } else {
      pending.resolve(msg.result);
    }
  }

  private _request(method: string, params: unknown): Promise<unknown> {
    const id = ++this._requestId;
    return new Promise((resolve, reject) => {
      this._pending.set(id, { resolve, reject });
      this._transport
        .send({ jsonrpc: "2.0", id, method, params })
        .catch(reject);
    });
  }
}
