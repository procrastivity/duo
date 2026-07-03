import { describe, expect, it, beforeEach } from "vitest";
import { Writable } from "stream";
import { createLogger, Logger } from "./logger.js";

/**
 * Helper to capture JSON lines written to a destination.
 * Each line is expected to be valid JSON.
 */
class LogCapture extends Writable {
  lines: string[] = [];

  _write(
    chunk: Buffer,
    _encoding: BufferEncoding,
    callback: (error?: Error | null) => void
  ): void {
    this.lines.push(chunk.toString("utf8"));
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
        requested_preset: "builder",
        preset_used: "builder",
        selected_tool_id: 42,
        fell_back_to_default: false,
        relented_on_avoid_provider: false,
      });

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(1);
      const log = jsons[0];

      const expectedKeys = [
        "level",
        "time",
        "event",
        "requested_preset",
        "preset_used",
        "selected_tool_id",
        "fell_back_to_default",
        "relented_on_avoid_provider",
      ].sort();
      expect(Object.keys(log).sort()).toEqual(expectedKeys);

      expect(log.event).toBe("resolution.success");
      expect(log.requested_preset).toBe("builder");
      expect(log.preset_used).toBe("builder");
      expect(log.selected_tool_id).toBe(42);
      expect(log.fell_back_to_default).toBe(false);
      expect(log.relented_on_avoid_provider).toBe(false);
    });

    it("rejects free-form fields via allow-list (runtime)", () => {
      const fields: any = {
        requested_preset: "builder",
        preset_used: "default",
        selected_tool_id: 1,
        fell_back_to_default: true,
        relented_on_avoid_provider: true,
        // Attempt to sneak in prohibited fields
        prompt: "do something bad",
        task: "malicious task",
        project_id: "secret-tenant-123",
        requested_name: "sneaky",
      };

      logger.resolutionSuccess(fields);

      const log = capture.getJsons()[0];
      expect(log).not.toHaveProperty("prompt");
      expect(log).not.toHaveProperty("task");
      expect(log).not.toHaveProperty("project_id");
      expect(log).not.toHaveProperty("requested_name");
    });

    it("carries the fallback + relent flags through", () => {
      logger.resolutionSuccess({
        requested_preset: "planner",
        preset_used: "default",
        selected_tool_id: 7,
        fell_back_to_default: true,
        relented_on_avoid_provider: true,
      });
      const log = capture.getJsons()[0];
      expect(log.preset_used).toBe("default");
      expect(log.fell_back_to_default).toBe(true);
      expect(log.relented_on_avoid_provider).toBe(true);
    });
  });

  describe("resolutionFailure", () => {
    it("logs resolution failure with unknown_preset error_code", () => {
      logger.resolutionFailure({
        requested_preset: "nope",
        error_code: "unknown_preset",
      });

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(1);
      const log = jsons[0];

      const expectedKeys = [
        "level",
        "time",
        "event",
        "requested_preset",
        "error_code",
      ].sort();
      expect(Object.keys(log).sort()).toEqual(expectedKeys);

      expect(log.event).toBe("resolution.failure");
      expect(log.requested_preset).toBe("nope");
      expect(log.error_code).toBe("unknown_preset");
    });

    it("logs resolution failure with preset_unavailable error_code", () => {
      logger.resolutionFailure({
        requested_preset: "builder",
        error_code: "preset_unavailable",
      });
      const log = capture.getJsons()[0];
      expect(log.error_code).toBe("preset_unavailable");
    });

    it("rejects free-form fields via allow-list", () => {
      const fields: any = {
        requested_preset: "builder",
        error_code: "preset_unavailable",
        prompt: "do something bad",
        project_id: "secret-tenant-123",
        requested_name: "sneaky",
      };
      logger.resolutionFailure(fields);
      const log = capture.getJsons()[0];
      expect(log).not.toHaveProperty("prompt");
      expect(log).not.toHaveProperty("project_id");
      expect(log).not.toHaveProperty("requested_name");
    });

    it("supports forward-compatible error_code values", () => {
      logger.resolutionFailure({
        requested_preset: "builder",
        error_code: "custom_solo_error_XYZ",
      });
      const log = capture.getJsons()[0];
      expect(log.error_code).toBe("custom_solo_error_XYZ");
    });
  });

  describe("spawnSuccess", () => {
    it("logs spawn success with correct shape and allow-list", () => {
      logger.spawnSuccess({
        requested_preset: "builder",
        selected_tool_id: 99,
        solo_process_id: "proc-12345",
        process_name: "opencode-ghc-sonnet--67890",
      });

      const jsons = capture.getJsons();
      expect(jsons).toHaveLength(1);
      const log = jsons[0];

      const expectedKeys = [
        "level",
        "time",
        "event",
        "requested_preset",
        "selected_tool_id",
        "solo_process_id",
        "process_name",
      ].sort();
      expect(Object.keys(log).sort()).toEqual(expectedKeys);

      expect(log.event).toBe("spawn.success");
      expect(log.requested_preset).toBe("builder");
      expect(log.selected_tool_id).toBe(99);
      expect(log.solo_process_id).toBe("proc-12345");
      expect(log.process_name).toBe("opencode-ghc-sonnet--67890");
    });

    it("rejects free-form fields via allow-list", () => {
      const fields: any = {
        requested_preset: "builder",
        selected_tool_id: 1,
        solo_process_id: "proc-123",
        process_name: "tool-proc",
        prompt: "do something bad",
        project_id: "secret-tenant-123",
        requested_name: "sneaky",
      };
      logger.spawnSuccess(fields);
      const log = capture.getJsons()[0];
      expect(log).not.toHaveProperty("prompt");
      expect(log).not.toHaveProperty("project_id");
      expect(log).not.toHaveProperty("requested_name");
    });
  });

  describe("timestamp and level formatting", () => {
    it("includes ISO 8601 timestamp in every log", () => {
      logger.resolutionSuccess({
        requested_preset: "builder",
        preset_used: "builder",
        selected_tool_id: 1,
        fell_back_to_default: false,
        relented_on_avoid_provider: false,
      });
      const log = capture.getJsons()[0];
      const timestamp = log.time as string;
      expect(timestamp).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
    });

    it("level field is string 'info', not numeric", () => {
      logger.resolutionSuccess({
        requested_preset: "builder",
        preset_used: "builder",
        selected_tool_id: 1,
        fell_back_to_default: false,
        relented_on_avoid_provider: false,
      });
      const log = capture.getJsons()[0];
      expect(log.level).toBe("info");
    });
  });

  describe("multiple log calls", () => {
    it("captures multiple log lines independently", () => {
      logger.resolutionSuccess({
        requested_preset: "builder",
        preset_used: "builder",
        selected_tool_id: 1,
        fell_back_to_default: false,
        relented_on_avoid_provider: false,
      });
      logger.resolutionFailure({
        requested_preset: "invalid",
        error_code: "unknown_preset",
      });
      logger.spawnSuccess({
        requested_preset: "builder",
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
