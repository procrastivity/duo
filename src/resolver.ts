import {
  PresetUnavailableError,
  UnknownPresetError,
  type PresetUnavailableDiagnostics,
} from "./errors.js";
import { isProviderEnabled as defaultIsProviderEnabled } from "./state/providers.js";
import { tokenizeArgs } from "./tokenize-args.js";
import type { PresetDefinition, Presets } from "./types/presets.js";

// ---------------------------------------------------------------------------
// Preset engine (D2/D4). Selection is provider-aware random pick with `default`
// fallback and a soft `avoid_provider` preference.
// ---------------------------------------------------------------------------

export interface ResolvePresetOptions {
  /** Selection seam; default `Math.random`. */
  rng?: () => number;
  /** Soft preference — never causes a hard failure by itself (D4). */
  avoidProvider?: string;
  /**
   * Injected only so unit tests stay filesystem-free (OQ6). Defaults to
   * `state/providers.isProviderEnabled`, which reads fresh from disk per call —
   * so "read fresh per spawn, never cached" holds when it is invoked here per
   * definition at resolve time.
   */
  isProviderEnabled?: (provider: string) => boolean;
}

export interface ResolvedPreset {
  agent_tool_id: number;
  extra_args: string[];
  provider?: string;
  preset_requested: string;
  preset_used: string;
  fell_back_to_default: boolean;
  relented_on_avoid_provider: boolean;
}

const DEFAULT_PRESET = "default";

interface CandidateSet {
  defs: PresetDefinition[];
  isDefault: boolean;
  relented: boolean;
}

export const resolvePreset = (
  presets: Presets,
  presetName: string,
  options: ResolvePresetOptions = {},
): ResolvedPreset => {
  // Unknown preset → error before any eligibility/selection work (D4).
  if (!Object.prototype.hasOwnProperty.call(presets, presetName)) {
    throw new UnknownPresetError(presetName);
  }

  const rng = options.rng ?? Math.random;
  const avoid = options.avoidProvider;
  const isEnabled = options.isProviderEnabled ?? defaultIsProviderEnabled;

  const requestedDefs = presets[presetName] ?? [];
  const defaultPresent = Object.prototype.hasOwnProperty.call(
    presets,
    DEFAULT_PRESET,
  );
  const defaultDefs = presets[DEFAULT_PRESET] ?? [];

  // A no-provider definition (`provider === undefined`) passes every filter.
  const enabledOnly = (defs: PresetDefinition[]): PresetDefinition[] =>
    defs.filter((d) => d.provider === undefined || isEnabled(d.provider));
  const enabledAndNotAvoid = (defs: PresetDefinition[]): PresetDefinition[] =>
    defs.filter(
      (d) =>
        d.provider === undefined ||
        (isEnabled(d.provider) && d.provider !== avoid),
    );

  // D4 candidate-set ladder; the first non-empty set is the random pool.
  const ladder: CandidateSet[] =
    avoid === undefined
      ? [
          { defs: enabledOnly(requestedDefs), isDefault: false, relented: false },
          { defs: enabledOnly(defaultDefs), isDefault: true, relented: false },
        ]
      : [
          { defs: enabledAndNotAvoid(requestedDefs), isDefault: false, relented: false },
          { defs: enabledAndNotAvoid(defaultDefs), isDefault: true, relented: false },
          { defs: enabledOnly(requestedDefs), isDefault: false, relented: true },
          { defs: enabledOnly(defaultDefs), isDefault: true, relented: true },
        ];

  const chosenSet = ladder.find((set) => set.defs.length > 0);

  if (chosenSet === undefined) {
    const considered = [...requestedDefs, ...defaultDefs];
    const disabled = new Set<string>();
    for (const d of considered) {
      if (d.provider !== undefined && !isEnabled(d.provider)) {
        disabled.add(d.provider);
      }
    }
    const diagnostics: PresetUnavailableDiagnostics = {
      requested_preset: presetName,
      default_tried: defaultPresent,
      default_present: defaultPresent,
      disabled_providers: [...disabled].sort(),
      ...(avoid !== undefined ? { avoid_provider: avoid } : {}),
    };
    throw new PresetUnavailableError(diagnostics);
  }

  const pool = chosenSet.defs;
  const idx = Math.floor(rng() * pool.length);
  const safeIdx = Math.min(Math.max(idx, 0), pool.length - 1);
  const selected = pool[safeIdx]!;

  return {
    agent_tool_id: selected.agent_tool_id,
    extra_args:
      selected.extra_args !== undefined ? tokenizeArgs(selected.extra_args) : [],
    ...(selected.provider !== undefined ? { provider: selected.provider } : {}),
    preset_requested: presetName,
    preset_used: chosenSet.isDefault ? DEFAULT_PRESET : presetName,
    fell_back_to_default: chosenSet.isDefault,
    relented_on_avoid_provider: chosenSet.relented,
  };
};
