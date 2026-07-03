import { describe, expect, it, vi } from "vitest";
import { launchAgentHandler, LaunchAgentInputSchema } from "./launch-agent.js";
import { SoloClient, SoloClientError } from "../solo-client.js";
import type { Logger } from "../logger.js";
import {
  spawnSuccessNamed,
  spawnSuccessUnnamed,
  spawnSuccessWithProjectId,
  spawnSuccessFromEnvProjectId,
  spawnRejectionNameInUse,
  spawnRejectionPermissionDenied,
} from "../__fixtures__/spawn-results.js";
import type { SoloSpawnResult } from "../types/solo.js";
import type { Presets } from "../types/presets.js";
import type { ResolvePresetOptions } from "../resolver.js";

interface MockClient {
  spawnProcess: ReturnType<typeof vi.fn>;
  projectId?: number;
}

const makeClient = (
  spawnResult: SoloSpawnResult | Error,
  projectId?: number,
): MockClient => ({
  spawnProcess:
    spawnResult instanceof Error
      ? vi.fn().mockRejectedValue(spawnResult)
      : vi.fn().mockResolvedValue(spawnResult),
  projectId,
});

const parse = (result: { content: Array<{ text: string }> }) =>
  JSON.parse(result.content[0].text);

const asClient = (m: MockClient) => m as unknown as SoloClient;

const allEnabled = (): boolean => true;
const seededRng =
  (v: number): (() => number) =>
  () =>
    v;
const opts: ResolvePresetOptions = { isProviderEnabled: allEnabled, rng: seededRng(0) };

// A single no-provider def per preset → deterministic selection.
const presets: Presets = {
  builder: [{ id: "b", agent_tool_id: 2 }],
  withArgs: [{ id: "wa", agent_tool_id: 3, extra_args: "--model sonnet --json" }],
  provided: [{ id: "p", agent_tool_id: 4, provider: "anthropic" }],
};

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

describe("launchAgentHandler", () => {
  describe("happy path, named", () => {
    it("calls spawnProcess with the resolved id + name, returns preset shape", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder", name: "my-helper" },
        presets,
        opts,
      );

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.process_id).toBe(spawnSuccessNamed.process_id);
      expect(data.name).toBe("my-helper");
      expect(data.preset).toBe("builder");
      expect(data.agent_tool_id).toBe(2);
      expect(data.extra_args).toEqual([]);
      expect(data.project_id).toBeUndefined();

      expect(client.spawnProcess).toHaveBeenCalledTimes(1);
      expect(client.spawnProcess.mock.calls[0][0]).toEqual({
        kind: "agent",
        agent_tool_id: 2,
        name: "my-helper",
      });
    });
  });

  describe("happy path, unnamed", () => {
    it("calls spawnProcess without a name key; result name comes from Solo", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessUnnamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder" },
        presets,
        opts,
      );

      expect(result.isError).toBeFalsy();
      expect(parse(result).name).toBe("agent-1234");
      const args = client.spawnProcess.mock.calls[0][0];
      expect(args).not.toHaveProperty("name");
      expect(args).not.toHaveProperty("project_id");
    });
  });

  describe("extra_args threading (deliverable c meets a)", () => {
    it("populates spawnArgs.extra_args from the resolved, tokenized extra_args", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "withArgs" },
        presets,
        opts,
      );

      expect(result.isError).toBeFalsy();
      const args = client.spawnProcess.mock.calls[0][0];
      expect(args.extra_args).toEqual(["--model", "sonnet", "--json"]);
      expect(parse(result).extra_args).toEqual(["--model", "sonnet", "--json"]);
    });

    it("omits extra_args from the spawn call when the preset has none", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder" },
        presets,
        opts,
      );
      expect(client.spawnProcess.mock.calls[0][0]).not.toHaveProperty(
        "extra_args",
      );
    });
  });

  describe("caller extra_args append (D3)", () => {
    it("appends caller extra_args AFTER the preset's resolved args (order preserved), on the spawn call and in the result", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "withArgs", extra_args: ["--verbose", "--flag"] },
        presets,
        opts,
      );

      expect(result.isError).toBeFalsy();
      const expected = ["--model", "sonnet", "--json", "--verbose", "--flag"];
      // Merged array actually reaches the spawned Solo process...
      expect(client.spawnProcess.mock.calls[0][0].extra_args).toEqual(expected);
      // ...and is echoed in the result.
      expect(parse(result).extra_args).toEqual(expected);
    });

    it("caller extra_args reach the spawn call even when the preset has none (caller-only)", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder", extra_args: ["--only-caller"] },
        presets,
        opts,
      );

      expect(result.isError).toBeFalsy();
      expect(client.spawnProcess.mock.calls[0][0].extra_args).toEqual([
        "--only-caller",
      ]);
      expect(parse(result).extra_args).toEqual(["--only-caller"]);
    });

    it("omits extra_args from the spawn call when both preset and caller are empty", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder", extra_args: [] },
        presets,
        opts,
      );
      expect(result.isError).toBeFalsy();
      expect(client.spawnProcess.mock.calls[0][0]).not.toHaveProperty(
        "extra_args",
      );
      expect(parse(result).extra_args).toEqual([]);
    });
  });

  describe("provider always reported (D4/OQ2)", () => {
    it("result.provider equals the label when the selected definition has one", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "provided" },
        presets,
        opts,
      );
      expect(parse(result).provider).toBe("anthropic");
    });

    it("result.provider is null when the selected definition has no provider", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder" },
        presets,
        opts,
      );
      const data = parse(result);
      // Key is always present (required), null for a no-provider definition.
      expect(data).toHaveProperty("provider");
      expect(data.provider).toBeNull();
    });
  });

  describe("avoid_provider threading (D5)", () => {
    it("relents to the avoided provider when it is the only eligible definition (relented_on_avoid_provider surfaces)", async () => {
      const { logger, calls } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      // Only one def, carrying the avoided provider → soft-avoid relents to it.
      const p: Presets = {
        solo: [{ id: "only", agent_tool_id: 9, provider: "anthropic" }],
      };
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "solo", avoid_provider: "anthropic" },
        p,
        opts,
      );

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(data.provider).toBe("anthropic");
      expect(data.agent_tool_id).toBe(9);
      const resFields = calls[0].fields as Record<string, unknown>;
      expect(resFields.relented_on_avoid_provider).toBe(true);
    });

    it("steers away from the avoided provider when an alternative exists", async () => {
      const { logger, calls } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const p: Presets = {
        multi: [
          { id: "a", agent_tool_id: 10, provider: "anthropic" },
          { id: "o", agent_tool_id: 11, provider: "openai" },
        ],
      };
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "multi", avoid_provider: "anthropic" },
        p,
        opts,
      );

      expect(result.isError).toBeFalsy();
      const data = parse(result);
      // Steered to the non-avoided provider — did not relent.
      expect(data.provider).toBe("openai");
      expect(data.agent_tool_id).toBe(11);
      const resFields = calls[0].fields as Record<string, unknown>;
      expect(resFields.relented_on_avoid_provider).toBe(false);
    });
  });

  describe("project_id propagation", () => {
    it("caller-supplied project_id is passed through to spawnProcess", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessWithProjectId);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder", name: "my-helper", project_id: 7 },
        presets,
        opts,
      );
      expect(parse(result).project_id).toBe(7);
      expect(client.spawnProcess.mock.calls[0][0].project_id).toBe(7);
    });

    it("client.projectId surfaces in result when caller omits project_id", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessFromEnvProjectId, 6);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder" },
        presets,
        opts,
      );
      expect(parse(result).project_id).toBe(6);
      expect(client.spawnProcess.mock.calls[0][0]).not.toHaveProperty(
        "project_id",
      );
    });
  });

  describe("schema rejection", () => {
    it("non-integer project_id rejected by schema", () => {
      expect(
        LaunchAgentInputSchema.safeParse({ preset: "builder", project_id: 1.5 })
          .success,
      ).toBe(false);
    });

    it("empty-string name rejected by schema", () => {
      expect(
        LaunchAgentInputSchema.safeParse({ preset: "builder", name: "" }).success,
      ).toBe(false);
    });

    it("non-string extra_args entries rejected by schema", () => {
      expect(
        LaunchAgentInputSchema.safeParse({
          preset: "builder",
          extra_args: [1, 2],
        }).success,
      ).toBe(false);
    });
  });

  describe("preset errors", () => {
    it("unknown preset returns unknown_preset and does not call spawnProcess", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "nope" },
        presets,
        opts,
      );
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("unknown_preset");
      expect(client.spawnProcess).not.toHaveBeenCalled();
    });

    it("preset unavailable returns preset_unavailable with diagnostics, no spawn", async () => {
      const { logger } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      const p: Presets = {
        builder: [{ id: "b", agent_tool_id: 2, provider: "openai" }],
      };
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder" },
        p,
        { isProviderEnabled: (prov) => prov !== "openai" },
      );
      expect(result.isError).toBe(true);
      const data = parse(result);
      expect(data.code).toBe("preset_unavailable");
      expect(data.diagnostics.requested_preset).toBe("builder");
      expect(client.spawnProcess).not.toHaveBeenCalled();
    });
  });

  describe("Solo spawn rejections", () => {
    it("name in use → spawn_rejected with solo_code and request echo", async () => {
      const { logger } = makeFakeLogger();
      const err = new SoloClientError(
        spawnRejectionNameInUse.message,
        spawnRejectionNameInUse.code,
      );
      const client = makeClient(err);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder", name: "my-helper" },
        presets,
        opts,
      );
      expect(result.isError).toBe(true);
      const data = parse(result);
      expect(data.code).toBe("spawn_rejected");
      expect(data.data.solo_code).toBe(-32602);
      expect(data.data.requested_preset).toBe("builder");
      expect(data.data.requested_name).toBe("my-helper");
      expect(data.data.agent_tool_id).toBe(2);
    });

    it("permission denied with caller project_id → echoes requested_project_id", async () => {
      const { logger } = makeFakeLogger();
      const err = new SoloClientError(
        spawnRejectionPermissionDenied.message,
        spawnRejectionPermissionDenied.code,
      );
      const client = makeClient(err);
      const result = await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder", project_id: 99 },
        presets,
        opts,
      );
      expect(result.isError).toBe(true);
      const data = parse(result);
      expect(data.code).toBe("spawn_rejected");
      expect(data.data.requested_project_id).toBe(99);
    });
  });

  describe("Logger instrumentation", () => {
    it("happy path — one resolutionSuccess followed by one spawnSuccess (order)", async () => {
      const { logger, calls } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder", name: "my-helper" },
        presets,
        opts,
      );

      expect(calls).toHaveLength(2);
      expect(calls[0].method).toBe("resolutionSuccess");
      expect(calls[1].method).toBe("spawnSuccess");

      const resFields = calls[0].fields as Record<string, unknown>;
      expect(resFields).toHaveProperty("requested_preset", "builder");
      expect(resFields).toHaveProperty("selected_tool_id", 2);

      const spawnFields = calls[1].fields as Record<string, unknown>;
      expect(spawnFields).toHaveProperty("requested_preset", "builder");
      expect(spawnFields).toHaveProperty("selected_tool_id", 2);
      expect(spawnFields).toHaveProperty("solo_process_id");
      expect(spawnFields).toHaveProperty("process_name");

      for (const call of calls) {
        const fields = call.fields as Record<string, unknown>;
        expect(fields).not.toHaveProperty("requested_name");
        expect(fields).not.toHaveProperty("requested_project_id");
        expect(fields).not.toHaveProperty("prompt");
      }
    });

    it("unknown preset — one resolutionFailure, no spawnSuccess", async () => {
      const { logger, calls } = makeFakeLogger();
      const client = makeClient(spawnSuccessNamed);
      await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "nope" },
        presets,
        opts,
      );
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionFailure");
      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_preset", "nope");
      expect(fields).toHaveProperty("error_code", "unknown_preset");
    });

    it("Solo spawn rejects — one resolutionSuccess, then NO spawnSuccess", async () => {
      const { logger, calls } = makeFakeLogger();
      const err = new SoloClientError(
        spawnRejectionNameInUse.message,
        spawnRejectionNameInUse.code,
      );
      const client = makeClient(err);
      await launchAgentHandler(
        asClient(client),
        logger,
        { preset: "builder", name: "my-helper" },
        presets,
        opts,
      );
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionSuccess");
    });
  });
});
