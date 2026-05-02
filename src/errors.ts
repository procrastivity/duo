import type { Tier, TokenSource } from "./classifier.js";

export const TIER_LABELS: readonly Tier[] = ["small", "medium", "large"];

export interface IgnoredToolDiagnostic {
  agent_tool_id: number;
  tool_name: string;
  tool_type: string;
  reason: "ambiguous" | "unclassifiable" | "wrong_tier" | "excluded";
  detected_tier?: Tier;
  matched_tokens?: string[];
  match_source?: TokenSource;
}

export interface ResolverDiagnostics {
  requested_tier: Tier;
  total_tools: number;
  enabled_count: number;
  excluded_count: number;
  ambiguous_count: number;
  unclassifiable_count: number;
  candidates_considered: number;
  strategy: "random" | "custom";
  override_token_count: number;
  preference_applied: boolean;
}

export interface TierUnavailableDiagnostics extends ResolverDiagnostics {
  ignored_tools: IgnoredToolDiagnostic[];
}

export class UnsupportedTierError extends Error {
  readonly code = "unsupported_tier" as const;
  readonly requested: string;
  readonly supported: readonly Tier[];

  constructor(requested: string) {
    super(
      `Unsupported tier "${requested}". Supported tiers: ${TIER_LABELS.join(", ")}.`,
    );
    this.name = "UnsupportedTierError";
    this.requested = requested;
    this.supported = TIER_LABELS;
  }
}

export class TierUnavailableError extends Error {
  readonly code = "tier_unavailable" as const;
  readonly diagnostics: TierUnavailableDiagnostics;

  constructor(diagnostics: TierUnavailableDiagnostics) {
    super(
      `No enabled candidates available for tier "${diagnostics.requested_tier}".`,
    );
    this.name = "TierUnavailableError";
    this.diagnostics = diagnostics;
  }
}

export class InvalidResolverOptionsError extends Error {
  readonly code = "invalid_resolver_options" as const;

  constructor(message: string) {
    super(message);
    this.name = "InvalidResolverOptionsError";
  }
}
