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

/**
 * Thrown when a requested preset name is not present in the config's `presets`
 * map. Checked before any eligibility/selection work (D4).
 */
export class UnknownPresetError extends Error {
  readonly code = "unknown_preset" as const;
  readonly preset: string;

  constructor(preset: string) {
    super(`Unknown preset "${preset}".`);
    this.name = "UnknownPresetError";
    this.preset = preset;
  }
}

export interface PresetUnavailableDiagnostics {
  /** The preset name the caller requested. */
  requested_preset: string;
  /** Whether the `default` preset was consulted as a fallback candidate set. */
  default_tried: boolean;
  /** Whether a `default` preset exists in the config at all. */
  default_present: boolean;
  /**
   * Providers referenced by the considered definitions that were disabled at
   * resolve time (deduped, sorted) — the usual reason no candidate survived.
   */
  disabled_providers: string[];
  /** The soft `avoid_provider` preference in effect, if any. */
  avoid_provider?: string;
}

/**
 * Thrown when no eligible definition survives selection for the requested
 * preset and its `default` fallback (D4). Carries diagnostics naming the
 * disabled providers so the failure is actionable. Note: `avoid_provider`
 * alone never causes this — the D4 ladder relents past the avoid filter first.
 */
export class PresetUnavailableError extends Error {
  readonly code = "preset_unavailable" as const;
  readonly diagnostics: PresetUnavailableDiagnostics;

  constructor(diagnostics: PresetUnavailableDiagnostics) {
    const providers = diagnostics.disabled_providers.length
      ? ` Disabled providers: ${diagnostics.disabled_providers.join(", ")}.`
      : "";
    super(
      `No enabled definition available for preset "${diagnostics.requested_preset}".${providers}`,
    );
    this.name = "PresetUnavailableError";
    this.diagnostics = diagnostics;
  }
}
