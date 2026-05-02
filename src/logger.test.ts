import { describe, expect, it, beforeEach } from "vitest";
import { Writable } from "stream";
import { createLogger, Logger } from "./logger.js";
import type {
  ResolutionSuccessLog,
  ResolutionFailureLog,
  SpawnSuccessLog,
} from "./logger.js";

/**
 * Helper to capture JSON lines written to a destination.
 * Each line is expected to be valid JSON.
 */
class LogCapture extends Writable {
  lines: string[] = [];

  constructor() {
    super();
  }

  _write(
    chunk: Buffer,
    _encoding: BufferEncoding,
    callback: (error?: Error | null) => void
  ): void {
    // Pino writes buffers; convert to UTF-8 string
    const text = chunk.toString("utf8");
    this.lines.push(text);
    callback();
  }

  getJsons(): Record<string, unknown>[] {
    return this.lines
      .map((line) => line.trim())
      .filter((line) => line.length > 0)
      .map((line) => JSON.parse(line));
  }
}

describe("Logger (pino to stderr)", () => {
  let capture: LogCapture;
  let logger: Logger;

  beforeEach(() => {
    capture = new LogCapture();
    logger = createLogger(capture);
  });

  describe("resolutionSuccess", () => {
    it("logs resolution success with correct shape and allow-list", () => {
      logger.resolutionSuccess({
        requested_tier: "medium",
        selected_tool_id: 42,
        selected_tool_name: "test-tool",
        match_source: "command",
        candidate_count: 3,
        token_source: "built_in",
        strategy: "random",
        preference_applied: false,
      });

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(1);

      const log = jsons[0];

      // Assert exact field set (allow-list)
      const expectedKeys = [
        "level",
        "time",
        "event",
        "requested_tier",
        "selected_tool_id",
        "selected_tool_name",
        "match_source",
        "candidate_count",
        "token_source",
        "strategy",
        "preference_applied",
      ].sort();

      const actualKeys = Object.keys(log).sort();
      expect(actualKeys).toEqual(expectedKeys);

      // Assert field values
      expect(log.event).toBe("resolution.success");
      expect(log.requested_tier).toBe("medium");
      expect(log.selected_tool_id).toBe(42);
      expect(log.selected_tool_name).toBe("test-tool");
      expect(log.match_source).toBe("command");
      expect(log.candidate_count).toBe(3);
      expect(log.token_source).toBe("built_in");
      expect(log.strategy).toBe("random");
      expect(log.preference_applied).toBe(false);
    });

    it("rejects free-form fields via allow-list (TypeScript + runtime)", () => {
      // TypeScript prevents this at compile time, but we test the runtime behavior
      // by verifying that extra fields are not emitted even if passed.
      const fields: any = {
        requested_tier: "small",
        selected_tool_id: 1,
        selected_tool_name: "tool",
        match_source: "command",
        candidate_count: 1,
        token_source: "built_in",
        strategy: "random",
        preference_applied: true,
        // Attempt to sneak in prohibited fields
        prompt: "do something bad",
        task: "malicious task",
        project_id: "secret-tenant-123",
        requested_name: "sneaky",
      };

      logger.resolutionSuccess(fields);

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(1);
      const log = jsons[0];

      expect(log).not.toHaveProperty("prompt");
      expect(log).not.toHaveProperty("task");
      expect(log).not.toHaveProperty("project_id");
      expect(log).not.toHaveProperty("requested_name");
    });

    it("supports all tier combinations", () => {
      const tiers = ["small", "medium", "large"] as const;

      for (const tier of tiers) {
        capture = new LogCapture();
        logger = createLogger(capture);

        logger.resolutionSuccess({
          requested_tier: tier,
          selected_tool_id: 1,
          selected_tool_name: "tool",
          match_source: "command",
          candidate_count: 1,
          token_source: "built_in",
          strategy: "random",
          preference_applied: true,
        });

        const log = capture.getJsons()[0];
        expect(log.requested_tier).toBe(tier);
      }
    });

    it("supports both match_source values", () => {
      const sources = ["command", "name_fallback"] as const;

      for (const source of sources) {
        capture = new LogCapture();
        logger = createLogger(capture);

        logger.resolutionSuccess({
          requested_tier: "medium",
          selected_tool_id: 1,
          selected_tool_name: "tool",
          match_source: source,
          candidate_count: 1,
          token_source: "built_in",
          strategy: "random",
          preference_applied: true,
        });

        const log = capture.getJsons()[0];
        expect(log.match_source).toBe(source);
      }
    });

    it("supports both token_source values", () => {
      const sources = ["built_in", "override"] as const;

      for (const source of sources) {
        capture = new LogCapture();
        logger = createLogger(capture);

        logger.resolutionSuccess({
          requested_tier: "medium",
          selected_tool_id: 1,
          selected_tool_name: "tool",
          match_source: "command",
          candidate_count: 1,
          token_source: source,
          strategy: "random",
          preference_applied: true,
        });

        const log = capture.getJsons()[0];
        expect(log.token_source).toBe(source);
      }
    });

    it("supports both strategy values", () => {
      const strategies = ["random", "custom"] as const;

      for (const strategy of strategies) {
        capture = new LogCapture();
        logger = createLogger(capture);

        logger.resolutionSuccess({
          requested_tier: "medium",
          selected_tool_id: 1,
          selected_tool_name: "tool",
          match_source: "command",
          candidate_count: 1,
          token_source: "built_in",
          strategy,
          preference_applied: true,
        });

        const log = capture.getJsons()[0];
        expect(log.strategy).toBe(strategy);
      }
    });
  });

  describe("resolutionFailure", () => {
    it("logs resolution failure with unsupported_tier error_code", () => {
      logger.resolutionFailure({
        requested_tier: "extra-large",
        error_code: "unsupported_tier",
        available_tiers: ["small", "medium", "large"],
      });

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(1);

      const log = jsons[0];

      // Assert exact field set (allow-list)
      const expectedKeys = [
        "level",
        "time",
        "event",
        "requested_tier",
        "error_code",
        "available_tiers",
      ].sort();

      const actualKeys = Object.keys(log).sort();
      expect(actualKeys).toEqual(expectedKeys);

      // Assert field values
      expect(log.event).toBe("resolution.failure");
      expect(log.requested_tier).toBe("extra-large");
      expect(log.error_code).toBe("unsupported_tier");
      expect(log.available_tiers).toEqual(["small", "medium", "large"]);
    });

    it("logs resolution failure with tier_unavailable error_code", () => {
      logger.resolutionFailure({
        requested_tier: "medium",
        error_code: "tier_unavailable",
        available_tiers: ["small"],
      });

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(1);

      const log = jsons[0];

      expect(log.event).toBe("resolution.failure");
      expect(log.requested_tier).toBe("medium");
      expect(log.error_code).toBe("tier_unavailable");
      expect(log.available_tiers).toEqual(["small"]);
    });

    it("rejects free-form fields via allow-list", () => {
      const fields: any = {
        requested_tier: "medium",
        error_code: "tier_unavailable",
        available_tiers: ["small"],
        // Attempt to sneak in prohibited fields
        prompt: "do something bad",
        task: "malicious task",
        project_id: "secret-tenant-123",
        requested_name: "sneaky",
      };

      logger.resolutionFailure(fields);

      const jsons = capture.getJsons();
      const log = jsons[0];

      expect(log).not.toHaveProperty("prompt");
      expect(log).not.toHaveProperty("task");
      expect(log).not.toHaveProperty("project_id");
      expect(log).not.toHaveProperty("requested_name");
    });

    it("supports forward-compatible error_code values", () => {
      logger.resolutionFailure({
        requested_tier: "medium",
        error_code: "custom_solo_error_XYZ",
        available_tiers: ["small"],
      });

      const log = capture.getJsons()[0];
      expect(log.error_code).toBe("custom_solo_error_XYZ");
    });
  });

  describe("spawnSuccess", () => {
    it("logs spawn success with correct shape and allow-list", () => {
      logger.spawnSuccess({
        requested_tier: "large",
        selected_tool_id: 99,
        solo_process_id: "proc-12345",
        process_name: "opencode-ghc-sonnet--67890",
      });

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(1);

      const log = jsons[0];

      // Assert exact field set (allow-list)
      const expectedKeys = [
        "level",
        "time",
        "event",
        "requested_tier",
        "selected_tool_id",
        "solo_process_id",
        "process_name",
      ].sort();

      const actualKeys = Object.keys(log).sort();
      expect(actualKeys).toEqual(expectedKeys);

      // Assert field values
      expect(log.event).toBe("spawn.success");
      expect(log.requested_tier).toBe("large");
      expect(log.selected_tool_id).toBe(99);
      expect(log.solo_process_id).toBe("proc-12345");
      expect(log.process_name).toBe("opencode-ghc-sonnet--67890");
    });

    it("rejects free-form fields via allow-list", () => {
      const fields: any = {
        requested_tier: "medium",
        selected_tool_id: 1,
        solo_process_id: "proc-123",
        process_name: "tool-proc",
        // Attempt to sneak in prohibited fields
        prompt: "do something bad",
        task: "malicious task",
        project_id: "secret-tenant-123",
        requested_name: "sneaky",
      };

      logger.spawnSuccess(fields);

      const jsons = capture.getJsons();
      const log = jsons[0];

      expect(log).not.toHaveProperty("prompt");
      expect(log).not.toHaveProperty("task");
      expect(log).not.toHaveProperty("project_id");
      expect(log).not.toHaveProperty("requested_name");
    });

    it("supports all tier combinations", () => {
      const tiers = ["small", "medium", "large"] as const;

      for (const tier of tiers) {
        capture = new LogCapture();
        logger = createLogger(capture);

        logger.spawnSuccess({
          requested_tier: tier,
          selected_tool_id: 1,
          solo_process_id: "proc-123",
          process_name: "tool-proc",
        });

        const log = capture.getJsons()[0];
        expect(log.requested_tier).toBe(tier);
      }
    });
  });

  describe("timestamp and level formatting", () => {
    it("includes ISO 8601 timestamp in every log", () => {
      logger.resolutionSuccess({
        requested_tier: "medium",
        selected_tool_id: 1,
        selected_tool_name: "tool",
        match_source: "command",
        candidate_count: 1,
        token_source: "built_in",
        strategy: "random",
        preference_applied: true,
      });

      const log = capture.getJsons()[0];
      expect(log).toHaveProperty("time");

      const timestamp = log.time as string;
      // ISO 8601 format: YYYY-MM-DDTHH:mm:ss.sssZ or similar
      const isoRegex = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
      expect(timestamp).toMatch(isoRegex);
    });

    it("level field is string 'info', not numeric", () => {
      logger.resolutionSuccess({
        requested_tier: "medium",
        selected_tool_id: 1,
        selected_tool_name: "tool",
        match_source: "command",
        candidate_count: 1,
        token_source: "built_in",
        strategy: "random",
        preference_applied: true,
      });

      const log = capture.getJsons()[0];
      expect(log.level).toBe("info");
      expect(typeof log.level).toBe("string");
    });
  });

  describe("multiple log calls", () => {
    it("captures multiple log lines independently", () => {
      logger.resolutionSuccess({
        requested_tier: "small",
        selected_tool_id: 1,
        selected_tool_name: "tool-a",
        match_source: "command",
        candidate_count: 1,
        token_source: "built_in",
        strategy: "random",
        preference_applied: false,
      });

      logger.resolutionFailure({
        requested_tier: "invalid",
        error_code: "unsupported_tier",
        available_tiers: ["small", "medium", "large"],
      });

      logger.spawnSuccess({
        requested_tier: "small",
        selected_tool_id: 1,
        solo_process_id: "proc-123",
        process_name: "tool-proc",
      });

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(3);

      expect(jsons[0].event).toBe("resolution.success");
      expect(jsons[1].event).toBe("resolution.failure");
      expect(jsons[2].event).toBe("spawn.success");
    });
  });
});
