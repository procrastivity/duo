import { describe, expect, it, vi } from "vitest";
import { resolveAgentToolHandler } from "./resolve-agent-tool.js";
import { SoloClient, SoloClientError } from "../solo-client.js";
import type { Logger } from "../logger.js";
import {
  accurateNameMisleadingCommand,
  disabledVariants,
  enabledRuntimes,
  misleadingNameAccurateCommand,
} from "../__fixtures__/agent-tools.js";
import type { SoloAgentTool } from "../types/solo.js";

const makeClient = (tools: SoloAgentTool[]) =>
  ({ listAgentTools: vi.fn().mockResolvedValue(tools) } as unknown as SoloClient);

const makeFailingClient = (err: Error) =>
  ({ listAgentTools: vi.fn().mockRejectedValue(err) } as unknown as SoloClient);

const parse = (result: { content: Array<{ text: string }> }) =>
  JSON.parse(result.content[0].text);

/**
 * Fake Logger that records all calls for assertion in tests.
 */
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

describe("resolveAgentToolHandler", () => {
  describe("happy path — medium tier against enabledRuntimes", () => {
    it("returns a medium-tier tool as selected", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), logger, { tier: "medium" });
      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(["codex-standard", "opencode-ghc-sonnet"]).toContain(data.selected.tool_name);
    });

    it("both medium-tier tools appear across selected and alternatives", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), logger, { tier: "medium" });
      const data = parse(result);
      const allNames = [
        data.selected.tool_name,
        ...data.alternatives.map((a: { tool_name: string }) => a.tool_name),
      ];
      expect(allNames).toContain("codex-standard");
      expect(allNames).toContain("opencode-ghc-sonnet");
    });
  });

  describe("unsupported tier", () => {
    it("returns isError with code unsupported_tier for an unknown tier label", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), logger, { tier: "purple" });
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("unsupported_tier");
    });

    it("message lists the supported tier labels", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), logger, { tier: "purple" });
      const { message } = parse(result);
      expect(message).toContain("small");
      expect(message).toContain("medium");
      expect(message).toContain("large");
    });
  });

  describe("tier unavailable — no matching tools", () => {
    const largeTierOnly: SoloAgentTool[] = [
      { id: 5, name: "codex-flagship", command: "codex --profile flagship", tool_type: "codex", enabled: true },
    ];

    it("returns isError with code tier_unavailable when no small-tier tools exist", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(largeTierOnly), logger, { tier: "small" });
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("tier_unavailable");
    });

    it("includes diagnostics block with the requested tier", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(largeTierOnly), logger, { tier: "small" });
      const data = parse(result);
      expect(data.diagnostics).toBeDefined();
      expect(data.diagnostics.requested_tier).toBe("small");
    });
  });

  describe("misleading-name fixture", () => {
    it("reports classification_source as command", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(
        makeClient([misleadingNameAccurateCommand]),
        logger,
        { tier: "large" },
      );
      expect(result.isError).toBeFalsy();
      expect(parse(result).classification_source).toBe("command");
    });

    it("matched_tokens includes the command's model token", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(
        makeClient([misleadingNameAccurateCommand]),
        logger,
        { tier: "large" },
      );
      const tokens = parse(result).selected.matched_tokens.map(
        (m: { token: string }) => m.token,
      );
      expect(tokens).toContain("opus");
    });
  });

  describe("accurate-name-misleading-command fixture", () => {
    it("reports classification_source as name_fallback", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(
        makeClient([accurateNameMisleadingCommand]),
        logger,
        { tier: "medium" },
      );
      expect(result.isError).toBeFalsy();
      expect(parse(result).classification_source).toBe("name_fallback");
    });
  });

  describe("disabled-variant fixture", () => {
    it("returns tier_unavailable when all matching tools are disabled", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(disabledVariants), logger, { tier: "medium" });
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("tier_unavailable");
    });

    it("diagnostics.enabled_count is 0 reflecting the disabled drop", async () => {
      const { logger } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(disabledVariants), logger, { tier: "medium" });
      expect(parse(result).diagnostics.enabled_count).toBe(0);
    });
  });

  describe("SoloClientError propagation", () => {
    it("returns isError with the underlying numeric code", async () => {
      const { logger } = makeFakeLogger();
      const soloError = new SoloClientError("MCP error -32603: Internal error", -32603);
      const result = await resolveAgentToolHandler(makeFailingClient(soloError), logger, { tier: "medium" });
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe(-32603);
    });
  });

  describe("Logger instrumentation", () => {
    it("happy path — one resolutionSuccess call with all expected fields", async () => {
      const { logger, calls } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), logger, { tier: "medium" });

      expect(result.isError).toBeFalsy();
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionSuccess");

      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_tier", "medium");
      expect(fields).toHaveProperty("selected_tool_id");
      expect(typeof fields.selected_tool_id).toBe("number");
      expect(fields).toHaveProperty("selected_tool_name");
      expect(typeof fields.selected_tool_name).toBe("string");
      expect(fields).toHaveProperty("match_source");
      expect(["command", "name_fallback"]).toContain(fields.match_source);
      expect(fields).toHaveProperty("candidate_count");
      expect(typeof fields.candidate_count).toBe("number");
      expect(fields).toHaveProperty("token_source");
      expect(["built_in", "override"]).toContain(fields.token_source);
      expect(fields).toHaveProperty("strategy");
      expect(["random", "custom"]).toContain(fields.strategy);
      expect(fields).toHaveProperty("preference_applied");
      expect(typeof fields.preference_applied).toBe("boolean");

      // Ensure no forbidden fields
      expect(fields).not.toHaveProperty("requested_name");
      expect(fields).not.toHaveProperty("requested_project_id");
      expect(fields).not.toHaveProperty("prompt");
    });

    it("unsupported tier — one resolutionFailure with error_code: unsupported_tier", async () => {
      const { logger, calls } = makeFakeLogger();
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), logger, { tier: "purple" });

      expect(result.isError).toBe(true);
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionFailure");

      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_tier", "purple");
      expect(fields).toHaveProperty("error_code", "unsupported_tier");
      expect(fields).toHaveProperty("available_tiers");
      expect(Array.isArray(fields.available_tiers)).toBe(true);

      // Ensure no forbidden fields
      expect(fields).not.toHaveProperty("requested_name");
      expect(fields).not.toHaveProperty("requested_project_id");
      expect(fields).not.toHaveProperty("prompt");
    });

    it("tier unavailable — one resolutionFailure with error_code: tier_unavailable", async () => {
      const { logger, calls } = makeFakeLogger();
      const largeTierOnly: SoloAgentTool[] = [
        { id: 5, name: "codex-flagship", command: "codex --profile flagship", tool_type: "codex", enabled: true },
      ];
      const result = await resolveAgentToolHandler(makeClient(largeTierOnly), logger, { tier: "small" });

      expect(result.isError).toBe(true);
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionFailure");

      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_tier", "small");
      expect(fields).toHaveProperty("error_code", "tier_unavailable");
      expect(fields).toHaveProperty("available_tiers");

      // Ensure no forbidden fields
      expect(fields).not.toHaveProperty("requested_name");
      expect(fields).not.toHaveProperty("requested_project_id");
      expect(fields).not.toHaveProperty("prompt");
    });

    it("listAgentTools fails — one resolutionFailure with error_code as string of Solo code", async () => {
      const { logger, calls } = makeFakeLogger();
      const soloError = new SoloClientError("MCP error -32603: Internal error", -32603);
      const result = await resolveAgentToolHandler(makeFailingClient(soloError), logger, { tier: "medium" });

      expect(result.isError).toBe(true);
      expect(calls).toHaveLength(1);
      expect(calls[0].method).toBe("resolutionFailure");

      const fields = calls[0].fields as Record<string, unknown>;
      expect(fields).toHaveProperty("requested_tier", "medium");
      expect(fields).toHaveProperty("error_code", "-32603");
      expect(fields).toHaveProperty("available_tiers");

      // Ensure no forbidden fields
      expect(fields).not.toHaveProperty("requested_name");
      expect(fields).not.toHaveProperty("requested_project_id");
      expect(fields).not.toHaveProperty("prompt");
    });
  });
});
