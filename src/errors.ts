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
