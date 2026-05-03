import type { Transport } from "./transport/types.js";
import {
  SoloAgentToolsSchema,
  SoloProjectsSchema,
  SoloSpawnResultSchema,
  type SoloAgentTool,
  type SoloProject,
  type SoloSpawnArgs,
  type SoloSpawnResult,
} from "./types/solo.js";
import {
  resolveProjectIdAtConnect,
  resolveProcessIdFromEnv,
} from "./solo-client/scope.js";

export type { SoloAgentTool, SoloProject, SoloSpawnArgs, SoloSpawnResult };

type EnvSource = Record<string, string | undefined>;

export interface ScopeLogger {
  info?: (msg: string, fields?: Record<string, unknown>) => void;
  warn?: (msg: string, fields?: Record<string, unknown>) => void;
}

export interface SoloClientOptions {
  cwd?: string;
  env?: EnvSource;
  logger?: ScopeLogger;
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
  private readonly _cwd: string;
  private readonly _env: EnvSource;
  private readonly _onInfo: (msg: string, fields?: Record<string, unknown>) => void;
  private readonly _onWarn: (msg: string, fields?: Record<string, unknown>) => void;
  private _requestId = 0;
  private readonly _pending = new Map<
    number,
    { resolve: (value: unknown) => void; reject: (error: Error) => void }
  >();
  private _projectId?: number;
  private _processId?: number;

  constructor(transport: Transport, options: SoloClientOptions = {}) {
    this._transport = transport;
    this._cwd = options.cwd ?? process.cwd();
    this._env = options.env ?? process.env;
    const noop = () => {};
    this._onInfo = options.logger?.info ?? noop;
    this._onWarn = options.logger?.warn ?? noop;
  }

  get projectId(): number | undefined {
    return this._projectId;
  }

  get processId(): number | undefined {
    return this._processId;
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

    await this._request("initialize", {
      protocolVersion: "2024-11-05",
      capabilities: {},
      clientInfo: { name: "duo", version: "0.1.0" },
    });
    await this._transport.send({
      jsonrpc: "2.0",
      method: "notifications/initialized",
    });

    await this._resolveScope();
  }

  private async _resolveScope(): Promise<void> {
    let projects: SoloProject[] = [];
    const envProjectId = this._env.SOLO_PROJECT_ID;

    // Only need projects list when env doesn't pin the answer.
    if (!envProjectId) {
      try {
        projects = await this.listProjects();
      } catch (err) {
        this._onWarn("solo.connect.list_projects_failed", {
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }

    const resolution = resolveProjectIdAtConnect(this._env, this._cwd, projects);
    this._projectId = resolution.projectId;

    if (
      resolution.envProjectId !== undefined &&
      resolution.pwdProjectId !== undefined &&
      resolution.envProjectId !== resolution.pwdProjectId
    ) {
      this._onInfo("solo.connect.project_scope_disagreement", {
        env_project_id: resolution.envProjectId,
        pwd_project_id: resolution.pwdProjectId,
        chose: resolution.envProjectId,
      });
    }

    if (this._projectId === undefined) {
      this._onInfo("solo.connect.project_unresolved", { cwd: this._cwd });
    } else {
      this._onInfo("solo.connect.project_resolved", {
        project_id: this._projectId,
        source: resolution.envProjectId !== undefined ? "env" : "pwd",
      });
    }

    const processId = resolveProcessIdFromEnv(this._env);
    if (processId !== undefined) {
      try {
        await this._bindSessionProcess(processId);
        this._processId = processId;
        this._onInfo("solo.connect.process_bound", { process_id: processId });
      } catch (err) {
        this._onWarn("solo.connect.bind_session_process_failed", {
          process_id: processId,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }
  }

  async disconnect(): Promise<void> {
    await this._transport.close();
  }

  async listAgentTools(): Promise<SoloAgentTool[]> {
    const result = await this._request("tools/call", {
      name: "list_agent_tools",
      arguments: {},
    });
    const text = this._extractText(result);
    return SoloAgentToolsSchema.parse(JSON.parse(text));
  }

  async listProjects(): Promise<SoloProject[]> {
    const result = await this._request("tools/call", {
      name: "list_projects",
      arguments: {},
    });
    const text = this._extractText(result);
    return SoloProjectsSchema.parse(JSON.parse(text));
  }

  async spawnProcess(args: SoloSpawnArgs): Promise<SoloSpawnResult> {
    const projectId = args.project_id ?? this._projectId;
    const callArgs: Record<string, unknown> = {
      kind: args.kind,
      agent_tool_id: args.agent_tool_id,
      ...(args.name !== undefined && { name: args.name }),
      ...(projectId !== undefined && { project_id: projectId }),
    };
    const result = await this._request("tools/call", {
      name: "spawn_process",
      arguments: callArgs,
    });
    const text = this._extractText(result);
    return SoloSpawnResultSchema.parse(JSON.parse(text));
  }

  private async _bindSessionProcess(processId: number): Promise<void> {
    await this._request("tools/call", {
      name: "bind_session_process",
      arguments: { process_id: processId },
    });
  }

  private _extractText(result: unknown): string {
    const response = result as {
      content?: Array<{ type: string; text?: string }>;
    };
    const textContent = response.content?.find((c) => c.type === "text");
    if (!textContent?.text) {
      throw new Error("tools/call returned no text content");
    }
    return textContent.text;
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
