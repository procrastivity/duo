import { describe, expect, it, vi } from "vitest";
import { resolveAgentToolHandler } from "./resolve-agent-tool.js";
import { SoloClient, SoloClientError } from "../solo-client.js";
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

describe("resolveAgentToolHandler", () => {
  describe("happy path — medium tier against enabledRuntimes", () => {
    it("returns a medium-tier tool as selected", async () => {
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), { tier: "medium" });
      expect(result.isError).toBeFalsy();
      const data = parse(result);
      expect(["codex-standard", "opencode-ghc-sonnet"]).toContain(data.selected.tool_name);
    });

    it("both medium-tier tools appear across selected and alternatives", async () => {
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), { tier: "medium" });
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
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), { tier: "purple" });
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("unsupported_tier");
    });

    it("message lists the supported tier labels", async () => {
      const result = await resolveAgentToolHandler(makeClient(enabledRuntimes), { tier: "purple" });
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
      const result = await resolveAgentToolHandler(makeClient(largeTierOnly), { tier: "small" });
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("tier_unavailable");
    });

    it("includes diagnostics block with the requested tier", async () => {
      const result = await resolveAgentToolHandler(makeClient(largeTierOnly), { tier: "small" });
      const data = parse(result);
      expect(data.diagnostics).toBeDefined();
      expect(data.diagnostics.requested_tier).toBe("small");
    });
  });

  describe("misleading-name fixture", () => {
    it("reports classification_source as command", async () => {
      const result = await resolveAgentToolHandler(
        makeClient([misleadingNameAccurateCommand]),
        { tier: "large" },
      );
      expect(result.isError).toBeFalsy();
      expect(parse(result).classification_source).toBe("command");
    });

    it("matched_tokens includes the command's model token", async () => {
      const result = await resolveAgentToolHandler(
        makeClient([misleadingNameAccurateCommand]),
        { tier: "large" },
      );
      expect(parse(result).matched_tokens).toContain("opus");
    });
  });

  describe("accurate-name-misleading-command fixture", () => {
    it("reports classification_source as name_fallback", async () => {
      const result = await resolveAgentToolHandler(
        makeClient([accurateNameMisleadingCommand]),
        { tier: "medium" },
      );
      expect(result.isError).toBeFalsy();
      expect(parse(result).classification_source).toBe("name_fallback");
    });
  });

  describe("disabled-variant fixture", () => {
    it("returns tier_unavailable when all matching tools are disabled", async () => {
      const result = await resolveAgentToolHandler(makeClient(disabledVariants), { tier: "medium" });
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe("tier_unavailable");
    });

    it("diagnostics.enabled_count is 0 reflecting the disabled drop", async () => {
      const result = await resolveAgentToolHandler(makeClient(disabledVariants), { tier: "medium" });
      expect(parse(result).diagnostics.enabled_count).toBe(0);
    });
  });

  describe("SoloClientError propagation", () => {
    it("returns isError with the underlying numeric code", async () => {
      const soloError = new SoloClientError("MCP error -32603: Internal error", -32603);
      const result = await resolveAgentToolHandler(makeFailingClient(soloError), { tier: "medium" });
      expect(result.isError).toBe(true);
      expect(parse(result).code).toBe(-32603);
    });
  });
});
