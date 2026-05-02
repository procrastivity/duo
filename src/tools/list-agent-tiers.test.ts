import { McpError } from "@modelcontextprotocol/sdk/types.js";
import { describe, expect, it, vi } from "vitest";
import {
  disabledVariants,
  enabledRuntimes,
  mixedRealistic,
} from "../__fixtures__/agent-tools.js";
import { SoloClientError } from "../solo-client.js";
import type { SoloAgentTool } from "../types/solo.js";
import { listAgentTiers } from "./list-agent-tiers.js";

const makeClient = (tools: SoloAgentTool[]) =>
  ({ listAgentTools: vi.fn().mockResolvedValue(tools) }) as any;

const makeErrorClient = (err: Error) =>
  ({ listAgentTools: vi.fn().mockRejectedValue(err) }) as any;

describe("listAgentTiers — all three tiers available", () => {
  it("reports available=true for all tiers with mixedRealistic fixture", async () => {
    const result = await listAgentTiers(makeClient(mixedRealistic));
    expect(result.small.available).toBe(true);
    expect(result.medium.available).toBe(true);
    expect(result.large.available).toBe(true);
  });

  it("each available tier has a default with required fields", async () => {
    const result = await listAgentTiers(makeClient(mixedRealistic));
    for (const tier of ["small", "medium", "large"] as const) {
      const t = result[tier];
      expect(t.available).toBe(true);
      expect(t.default).toBeDefined();
      expect(typeof t.default!.agent_tool_id).toBe("number");
      expect(typeof t.default!.tool_name).toBe("string");
      expect(typeof t.default!.tool_type).toBe("string");
      expect(typeof t.default!.command).toBe("string");
      expect(["command", "name_fallback"]).toContain(
        t.default!.classification_source,
      );
    }
  });

  it("each available tier has diagnostics with expected counts", async () => {
    const result = await listAgentTiers(makeClient(mixedRealistic));
    for (const tier of ["small", "medium", "large"] as const) {
      const d = result[tier].diagnostics;
      expect(d.requested_tier).toBe(tier);
      expect(d.total_tools).toBe(mixedRealistic.length);
      expect(d.strategy).toBe("random");
    }
  });
});

describe("listAgentTiers — unavailable tier", () => {
  it("reports available=false when no small candidates exist", async () => {
    // Remove small-tier tools (haiku id=1, fast id=3) from enabledRuntimes.
    const noSmall = enabledRuntimes.filter((t) => t.id !== 1 && t.id !== 3);
    const result = await listAgentTiers(makeClient(noSmall));
    expect(result.small.available).toBe(false);
    expect(result.small.default).toBeUndefined();
    expect(result.small.alternatives).toEqual([]);
  });

  it("unavailable tier includes diagnostics with candidates_considered=0", async () => {
    const noSmall = enabledRuntimes.filter((t) => t.id !== 1 && t.id !== 3);
    const result = await listAgentTiers(makeClient(noSmall));
    expect(result.small.diagnostics.candidates_considered).toBe(0);
    expect(result.small.diagnostics.requested_tier).toBe("small");
  });

  it("other tiers remain available when one tier is empty", async () => {
    const noSmall = enabledRuntimes.filter((t) => t.id !== 1 && t.id !== 3);
    const result = await listAgentTiers(makeClient(noSmall));
    expect(result.medium.available).toBe(true);
    expect(result.large.available).toBe(true);
  });

  it("all tiers report available=false when list is empty", async () => {
    const result = await listAgentTiers(makeClient([]));
    expect(result.small.available).toBe(false);
    expect(result.medium.available).toBe(false);
    expect(result.large.available).toBe(false);
  });
});

describe("listAgentTiers — disabled tools excluded from alternatives", () => {
  it("disabled tool ids never appear in any tier's alternatives", async () => {
    const result = await listAgentTiers(makeClient(mixedRealistic));
    // mixedRealistic has two explicitly disabled entries: id=21 and id=22
    const disabledInMixed = new Set([21, 22]);
    for (const tier of ["small", "medium", "large"] as const) {
      for (const alt of result[tier].alternatives) {
        expect(disabledInMixed.has(alt.agent_tool_id)).toBe(false);
      }
    }
  });

  it("disabled tools do not appear as the default for any tier", async () => {
    const result = await listAgentTiers(makeClient(mixedRealistic));
    const disabledInMixed = new Set([21, 22]);
    for (const tier of ["small", "medium", "large"] as const) {
      if (result[tier].default) {
        expect(disabledInMixed.has(result[tier].default!.agent_tool_id)).toBe(
          false,
        );
      }
    }
  });

  it("all-disabled list yields available=false for every tier", async () => {
    const result = await listAgentTiers(makeClient(disabledVariants));
    expect(result.small.available).toBe(false);
    expect(result.medium.available).toBe(false);
    expect(result.large.available).toBe(false);
  });

  it("diagnostics.enabled_count is 0 when all tools are disabled", async () => {
    const result = await listAgentTiers(makeClient(disabledVariants));
    for (const tier of ["small", "medium", "large"] as const) {
      expect(result[tier].diagnostics.enabled_count).toBe(0);
    }
  });
});

describe("listAgentTiers — SoloClientError propagation", () => {
  it("re-throws SoloClientError as McpError", async () => {
    const clientErr = new SoloClientError("MCP error -32603: transport failed", -32603);
    await expect(listAgentTiers(makeErrorClient(clientErr))).rejects.toBeInstanceOf(
      McpError,
    );
  });

  it("McpError preserves the original error code", async () => {
    const clientErr = new SoloClientError("MCP error -32000: closed", -32000);
    try {
      await listAgentTiers(makeErrorClient(clientErr));
      throw new Error("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(McpError);
      expect((err as McpError).code).toBe(-32000);
    }
  });

  it("non-SoloClientError is re-thrown as-is", async () => {
    const unexpected = new TypeError("unexpected failure");
    await expect(
      listAgentTiers(makeErrorClient(unexpected)),
    ).rejects.toBeInstanceOf(TypeError);
  });
});

describe("listAgentTiers — listAgentTools called exactly once", () => {
  it("invokes listAgentTools exactly once per call regardless of tier count", async () => {
    const client = makeClient(mixedRealistic);
    await listAgentTiers(client);
    expect(client.listAgentTools).toHaveBeenCalledTimes(1);
  });

  it("two separate calls each invoke listAgentTools once", async () => {
    const client = makeClient(mixedRealistic);
    await listAgentTiers(client);
    await listAgentTiers(client);
    expect(client.listAgentTools).toHaveBeenCalledTimes(2);
  });
});
