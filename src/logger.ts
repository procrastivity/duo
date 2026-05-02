import pino from "pino";

/**
 * Log emitted when tool resolution succeeds.
 * Allow-list enforced: only these fields are logged.
 */
export interface ResolutionSuccessLog {
  event: "resolution.success";
  requested_tier: "small" | "medium" | "large";
  selected_tool_id: number;
  selected_tool_name: string;
  match_source: "command" | "name_fallback";
  candidate_count: number;
  token_source: "built_in" | "override";
  strategy: "random" | "custom";
  preference_applied: boolean;
}

/**
 * Log emitted when tool resolution fails.
 * Allow-list enforced: only these fields are logged.
 */
export interface ResolutionFailureLog {
  event: "resolution.failure";
  requested_tier: string;
  error_code: "unsupported_tier" | "tier_unavailable" | string;
  available_tiers: ("small" | "medium" | "large")[];
}

/**
 * Log emitted when a process spawn succeeds.
 * Allow-list enforced: only these fields are logged.
 */
export interface SpawnSuccessLog {
  event: "spawn.success";
  requested_tier: "small" | "medium" | "large";
  selected_tool_id: number;
  solo_process_id: string;
  process_name: string;
}

/**
 * Logger interface: structured logging with allow-list discipline.
 * Each method accepts only its declared fields (without "event").
 * Callers cannot pass undeclared fields like prompt, task, project_id, or requested_name.
 */
export interface Logger {
  resolutionSuccess(
    fields: Omit<ResolutionSuccessLog, "event">
  ): void;
  resolutionFailure(
    fields: Omit<ResolutionFailureLog, "event">
  ): void;
  spawnSuccess(
    fields: Omit<SpawnSuccessLog, "event">
  ): void;
}

/**
 * Creates a Logger instance wrapping pino.
 * 
 * @param destination Optional pino destination stream. Defaults to stderr (fd 2).
 * @returns Logger instance with allow-list-enforced methods.
 * 
 * Configuration:
 * - level: "info" (single channel, no debug spam in v0)
 * - timestamp: ISO 8601 format (pino.stdTimeFunctions.isoTime)
 * - level formatter: emit "info" string instead of numeric
 * - base: empty object (no pid/hostname/etc auto-fields)
 * - destination: defaults to pino.destination(2) (stderr)
 */
export const createLogger = (
  destination?: pino.DestinationStream
): Logger => {
  const pinoLogger = pino(
    {
      level: "info",
      timestamp: pino.stdTimeFunctions.isoTime,
      formatters: {
        level: (label) => ({ level: label }),
      },
      base: {},
    },
    destination ?? pino.destination(2)
  );

  return {
    /**
     * Log successful tool resolution.
     * Destructures and re-emits ONLY the declared fields from ResolutionSuccessLog.
     */
    resolutionSuccess({
      requested_tier,
      selected_tool_id,
      selected_tool_name,
      match_source,
      candidate_count,
      token_source,
      strategy,
      preference_applied,
    }): void {
      pinoLogger.info({
        event: "resolution.success",
        requested_tier,
        selected_tool_id,
        selected_tool_name,
        match_source,
        candidate_count,
        token_source,
        strategy,
        preference_applied,
      });
    },

    /**
     * Log failed tool resolution.
     * Destructures and re-emits ONLY the declared fields from ResolutionFailureLog.
     */
    resolutionFailure({
      requested_tier,
      error_code,
      available_tiers,
    }): void {
      pinoLogger.info({
        event: "resolution.failure",
        requested_tier,
        error_code,
        available_tiers,
      });
    },

    /**
     * Log successful process spawn.
     * Destructures and re-emits ONLY the declared fields from SpawnSuccessLog.
     */
    spawnSuccess({
      requested_tier,
      selected_tool_id,
      solo_process_id,
      process_name,
    }): void {
      pinoLogger.info({
        event: "spawn.success",
        requested_tier,
        selected_tool_id,
        solo_process_id,
        process_name,
      });
    },
  };
};
