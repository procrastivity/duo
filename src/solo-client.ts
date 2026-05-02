import type { Transport } from "./transport/types.js";

export interface AgentTool {
  name: string;
  description?: string;
  inputSchema?: unknown;
}

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

  async listAgentTools(): Promise<AgentTool[]> {
    const result = await this._request("tools/list", {});
    const parsed = result as { tools?: AgentTool[] };
    return parsed.tools ?? [];
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
