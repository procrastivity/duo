import { describe, expect, it, vi } from "vitest";
import {
  spawnAgentHandler,
  SpawnAgentInputSchema,
} from "./spawn-agent.js";
import { SoloClient, SoloClientError } from "../solo-client.js";
import type { Logger } from "../logger.js";
import {
  enabledRuntimes,
  misleadingNameAccurateCommand,
  mixedRealistic,
} from "../__fixtures__/agent-tools.js";
import {
  spawnSuccessNamed,
  spawnSuccessUnnamed,
  spawnSuccessWithProjectId,
  spawnSuccessFromEnvProjectId,
  spawnRejectionNameInUse,
  spawnRejectionInvalidAgentToolId,
  spawnRejectionPermissionDenied,
} from "../__fixtures__/spawn-results.js";
import type { SoloAgentTool, SoloSpawnResult } from "../types/solo.js";

interface MockClient {
  listAgentTools: ReturnType<typeof vi.fn>;
  spawnProcess: ReturnType<typeof vi.fn>;
  projectId?: number;
}

const makeClient = (
  tools: SoloAgentTool[],
  spawnResult: SoloSpawnResult | Error,
  projectId?: number,
): MockClient => ({
  listAgentTools: vi.fn().mockResolvedValue(tools),
  spawnProcess:
    spawnResult instanceof Error
      ? vi.fn().mockRejectedValue(spawnResult)
      : vi.fn().mockResolvedValue(spawnResult),
  projectId,
});

const makeListFailingClient = (err: Error): MockClient => ({
  listAgentTools: vi.fn().mockRejectedValue(err),
  spawnProcess: vi.fn(),
});

const parse = (result: { content: Array<{ text: string }> }) =>
  JSON.parse(result.content[0].text);

const asClient = (m: MockClient) => m as unknown as SoloClient;

const justSonnetMedium: SoloAgentTool[] = [
  {
    id: 2,
    name: "opencode-ghc-sonnet",
    command: "opencode --model sonnet",
    tool_type: "opencode",
    enabled: true,
  },
];

const makeFakeLogger = () => {
  const calls: Array<{ method: string; fields: unknown }> = [];

  const logger: Logger = {
    resolutionSuccess(fields) {
      calls.push({ method: "resolutionSuccess", fields });
    },
    resolutionFailure(fields) {
      calls.push({ method: "resolutionFailure", fields });
    },
    spawnSuccess(fields) {
      calls.push({ method: "spawnSuccess", fields });
    },
  };

  return { logger, calls };
};

describe("spawnAgentHandler", () => {
  describe("happy path, named", () => {
    it("calls spawnProcess with name and no project_id, returns success shape", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(justSonnetMedium, spawnSuccessNamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
        name: "my-helper",
      });

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.process_id).toBe(spawnSuccessNamed.process_id);
      expect(data.name).toBe("my-helper");
      expect(data.tier).toBe("medium");
      expect(data.tool.agent_tool_id).toBe(2);
      expect(data.project_id).toBeUndefined();

      expect(client.spawnProcess).toHaveBeenCalledTimes(1);
      const args = client.spawnProcess.mock.calls[0][0];
      expect(args).toEqual({
        kind: "agent",
        agent_tool_id: 2,
        name: "my-helper",
      });
      expect(args).not.toHaveProperty("project_id");
    });
  });

  describe("happy path, unnamed", () => {
    it("calls spawnProcess without a name key; result name comes from Solo", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(justSonnetMedium, spawnSuccessUnnamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
      });

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.name).toBe("agent-1234");

      const args = client.spawnProcess.mock.calls[0][0];
      expect(args).not.toHaveProperty("name");
      expect(args).not.toHaveProperty("project_id");
    });
  });

  describe("project_id propagation", () => {
    it("caller-supplied project_id is passed through to spawnProcess", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(justSonnetMedium, spawnSuccessWithProjectId);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
        name: "my-helper",
        project_id: 7,
      });

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.project_id).toBe(7);

      expect(client.spawnProcess.mock.calls[0][0].project_id).toBe(7);
    });

    it("client.projectId surfaces in result when caller omits project_id", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(justSonnetMedium, spawnSuccessFromEnvProjectId, 6);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
      });

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.project_id).toBe(6);
      // Tool handler does NOT thread project_id into the call args anymore;
      // SoloClient.spawnProcess injects it from client.projectId.
      expect(client.spawnProcess.mock.calls[0][0]).not.toHaveProperty(
        "project_id",
      );
    });

    it("no project_id anywhere → omitted from call args and from result", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(justSonnetMedium, spawnSuccessUnnamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
      });

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.project_id).toBeUndefined();
      expect(client.spawnProcess.mock.calls[0][0]).not.toHaveProperty(
        "project_id",
      );
    });
  });

  describe("schema rejection", () => {
    it("non-integer project_id rejected by schema", () => {
      const parsed = SpawnAgentInputSchema.safeParse({
        tier: "medium",
        project_id: 1.5,
      });
      expect(parsed.success).toBe(false);
    });

    it("string project_id rejected by schema", () => {
      const parsed = SpawnAgentInputSchema.safeParse({
        tier: "medium",
        project_id: "6",
      });
      expect(parsed.success).toBe(false);
    });

    it("empty-string name rejected by schema", () => {
      const parsed = SpawnAgentInputSchema.safeParse({
        tier: "medium",
        name: "",
      });
      expect(parsed.success).toBe(false);
    });
  });

  describe("tier errors", () => {
    it("unknown tier returns unsupported_tier and does not call spawnProcess", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(enabledRuntimes, spawnSuccessNamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "huge",
      });

      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("unsupported_tier");
      expect(client.spawnProcess).not.toHaveBeenCalled();
    });

    it("tier unavailable returns tier_unavailable with diagnostics, does not call spawnProcess", async () => {
      const { logger } = makeFakeLogger();
      const largeOnly: SoloAgentTool[] = [
        {
          id: 5,
          name: "codex-flagship",
          command: "codex --profile flagship",
          tool_type: "codex",
          enabled: true,
        },
      ];
      const client = makeClient(largeOnly, spawnSuccessNamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "small",
      });

      expect(result.isError).toBe(true);
      const data = parse(result);
      expect(data.code).toBe("tier_unavailable");
      expect(data.diagnostics).toBeDefined();
      expect(data.diagnostics.requested_tier).toBe("small");
      expect(client.spawnProcess).not.toHaveBeenCalled();
    });
  });

  describe("Solo spawn rejections", () => {
    const expectSpawnRejected = (
      data: ReturnType<typeof parse>,
      expected: { solo_code: number; messageContains: string; tier: string },
    ) => {
      expect(data.code).toBe("spawn_rejected");
      expect(data.message).toContain(expected.messageContains);
      expect(data.data.solo_code).toBe(expected.solo_code);
      expect(data.data.requested_tier).toBe(expected.tier);
    };

    it("name in use → spawn_rejected with solo_code -32602 and request echo", async () => {
      const { logger } = makeFakeLogger();
      const err = new SoloClientError(
        spawnRejectionNameInUse.message,
        spawnRejectionNameInUse.code,
      );
      const client = makeClient(justSonnetMedium, err);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
        name: "my-helper",
      });

      expect(result.isError).toBe(true);
      const data = parse(result);
      expectSpawnRejected(data, {
        solo_code: -32602,
        messageContains: "already in use",
        tier: "medium",
      });
      expect(data.data.requested_name).toBe("my-helper");
      expect(data.data.agent_tool_id).toBe(2);
      expect(client.spawnProcess).toHaveBeenCalledTimes(1);
    });

    it("invalid agent_tool_id → same spawn_rejected path", async () => {
      const { logger } = makeFakeLogger();
      const err = new SoloClientError(
        spawnRejectionInvalidAgentToolId.message,
        spawnRejectionInvalidAgentToolId.code,
      );
      const client = makeClient(justSonnetMedium, err);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
      });

      expect(result.isError).toBe(true);
      const data = parse(result);
      expectSpawnRejected(data, {
        solo_code: -32602,
        messageContains: "agent_tool_id",
        tier: "medium",
      });
      expect(client.spawnProcess).toHaveBeenCalledTimes(1);
    });

    it("permission denied with caller project_id → spawn_rejected echoes requested_project_id", async () => {
      const { logger } = makeFakeLogger();
      const err = new SoloClientError(
        spawnRejectionPermissionDenied.message,
        spawnRejectionPermissionDenied.code,
      );
      const client = makeClient(justSonnetMedium, err);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
        project_id: 99,
      });

      expect(result.isError).toBe(true);
      const data = parse(result);
      expectSpawnRejected(data, {
        solo_code: -32603,
        messageContains: "permission denied",
        tier: "medium",
      });
      expect(data.data.requested_project_id).toBe(99);
    });
  });

  describe("listAgentTools failure", () => {
    it("Solo listAgentTools failure → spawnProcess never called; Solo code passthrough", async () => {
      const { logger } = makeFakeLogger();
      const err = new SoloClientError("MCP error -32000: Server error", -32000);
      const client = makeListFailingClient(err);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
      });

      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe(-32000);
      expect(client.spawnProcess).not.toHaveBeenCalled();
    });
  });

  describe("resolver receives full tools list incl. disabled", () => {
    it("listAgentTools called once with no filtering before resolver", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(mixedRealistic, spawnSuccessNamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
        name: "my-helper",
      });

      expect(result.isError).toBeFalsy();
      expect(client.listAgentTools).toHaveBeenCalledTimes(1);
    });
  });

  describe("tool summary echoes resolved tool", () => {
    it("includes tool_name, tool_type, command, and classification_source from resolution", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(justSonnetMedium, spawnSuccessNamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
        name: "my-helper",
      });

      const data = parse(result);
      expect(data.tool.tool_name).toBe("opencode-ghc-sonnet");
      expect(data.tool.tool_type).toBe("opencode");
      expect(data.tool.command).toBe("opencode --model sonnet");
      expect(data.tool.classification_source).toBe("command");
    });
  });

  describe("misleading-name fixture spawn", () => {
    it("uses resolved id and reports classification_source=command", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient([misleadingNameAccurateCommand], {
        ...spawnSuccessNamed,
        agent_tool_id: 10,
      });
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "large",
        name: "my-helper",
      });

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.tool.agent_tool_id).toBe(10);
      expect(data.tool.classification_source).toBe("command");

      expect(client.spawnProcess.mock.calls[0][0].agent_tool_id).toBe(10);
    });
  });

  describe("Logger instrumentation", () => {
    it("happy path — one resolutionSuccess followed by one spawnSuccess (assert order)", async () => {
      const { logger, calls } = makeFakeLogger();
      const client = makeClient(justSonnetMedium, spawnSuccessNamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
        name: "my-helper",
      });

      expect(result.isError).toBeFalsy();
      expect(calls).toHaveLength(2);
      expect(calls[0].method).toBe("resolutionSuccess");
      expect(calls[1].method).toBe("spawnSuccess");

      const resFields = calls[0].fields as Record<string, unknown>;
      expect(resFields).toHaveProperty("requested_tier", "medium");
      expect(resFields).toHaveProperty("selected_tool_id");
      expect(resFields).toHaveProperty("selected_tool_name");
      expect(resFields).toHaveProperty("match_source");
      expect(resFields).toHaveProperty("candidate_count");
      expect(resFields).toHaveProperty("token_source");
      expect(resFields).toHaveProperty("strategy");
      expect(resFields).toHaveProperty("preference_applied");

      const spawnFields = calls[1].fields as Record<string, unknown>;
      expect(spawnFields).toHaveProperty("requested_tier", "medium");
      expect(spawnFields).toHaveProperty("selected_tool_id");
      expect(spawnFields).toHaveProperty("solo_process_id");
      expect(spawnFields).toHaveProperty("process_name");

      expect(resFields).not.toHaveProperty("requested_name");
      expect(resFields).not.toHaveProperty("requested_project_id");
      expect(resFields).not.toHaveProperty("prompt");
      expect(spawnFields).not.toHaveProperty("requested_name");
      expect(spawnFields).not.toHaveProperty("requested_project_id");
      expect(spawnFields).not.toHaveProperty("prompt");
    });

    it("resolver fails (unsupported tier) — one resolutionFailure, no spawnSuccess", async () => {
      const { logger, calls } = makeFakeLogger();
      const client = makeClient(enabledRuntimes, spawnSuccessNamed);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "huge",
      });

      expect(result.isError).toBe(true);
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionFailure");

      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_tier", "huge");
      expect(fields).toHaveProperty("error_code", "unsupported_tier");
      expect(fields).toHaveProperty("available_tiers");

      expect(fields).not.toHaveProperty("requested_name");
      expect(fields).not.toHaveProperty("requested_project_id");
      expect(fields).not.toHaveProperty("prompt");
    });

    it("Solo spawn rejects — one resolutionSuccess, then NO further log calls", async () => {
      const { logger, calls } = makeFakeLogger();
      const err = new SoloClientError(
        spawnRejectionNameInUse.message,
        spawnRejectionNameInUse.code,
      );
      const client = makeClient(justSonnetMedium, err);
      const result = await spawnAgentHandler(asClient(client), logger, {
        tier: "medium",
        name: "my-helper",
      });

      expect(result.isError).toBe(true);
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionSuccess");

      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_tier", "medium");
      expect(fields).toHaveProperty("selected_tool_id");
      expect(fields).not.toHaveProperty("requested_name");
      expect(fields).not.toHaveProperty("requested_project_id");
      expect(fields).not.toHaveProperty("prompt");
    });

    it("sweeps all log calls: no requested_name, requested_project_id, or prompt in any log", async () => {
      const { logger: logger1, calls: calls1 } = makeFakeLogger();
      const client1 = makeClient(justSonnetMedium, spawnSuccessNamed);
      await spawnAgentHandler(asClient(client1), logger1, {
        tier: "medium",
        name: "my-helper",
        project_id: 123,
      });

      for (const call of calls1) {
        const fields = call.fields as Record<string, unknown>;
        expect(fields).not.toHaveProperty("requested_name");
        expect(fields).not.toHaveProperty("requested_project_id");
        expect(fields).not.toHaveProperty("prompt");
      }

      const { logger: logger2, calls: calls2 } = makeFakeLogger();
      const largeOnly: SoloAgentTool[] = [
        { id: 5, name: "codex-flagship", command: "codex --profile flagship", tool_type: "codex", enabled: true },
      ];
      const client2 = makeClient(largeOnly, spawnSuccessNamed);
      await spawnAgentHandler(asClient(client2), logger2, {
        tier: "small",
      });

      for (const call of calls2) {
        const fields = call.fields as Record<string, unknown>;
        expect(fields).not.toHaveProperty("requested_name");
        expect(fields).not.toHaveProperty("requested_project_id");
        expect(fields).not.toHaveProperty("prompt");
      }
    });
  });
});
