import {
  classify,
  type Classification,
  type ClassifierTokenPolicy,
  type Tier,
  type TokenSource,
} from "./classifier.js";
import {
  InvalidResolverOptionsError,
  PresetUnavailableError,
  TIER_LABELS,
  TierUnavailableError,
  UnknownPresetError,
  UnsupportedTierError,
  type IgnoredToolDiagnostic,
  type PresetUnavailableDiagnostics,
  type ResolverDiagnostics,
  type TierUnavailableDiagnostics,
} from "./errors.js";
import { isProviderEnabled as defaultIsProviderEnabled } from "./state/providers.js";
import { tokenizeArgs } from "./tokenize-args.js";
import type { PresetDefinition, Presets } from "./types/presets.js";
import type { PreferenceSelector } from "./types/policy.js";
import type { SoloAgentTool } from "./types/solo.js";

export type SelectionStrategy = "random" | "custom";

export interface ResolverOptions {
  strategy?: SelectionStrategy;
  excludeIds?: number[];
  rng?: () => number;
  preference?: PreferenceSelector[];
  classifierPolicy?: ClassifierTokenPolicy;
}

export interface MatchedToken {
  token: string;
  source: TokenSource;
}

export interface ResolutionSelected {
  agent_tool_id: number;
  tool_name: string;
  tool_type: string;
  command: string;
  token_source: TokenSource;
  matched_tokens: MatchedToken[];
}

export interface ResolutionAlternative {
  agent_tool_id: number;
  tool_name: string;
  tool_type: string;
  classification_source: "command" | "name_fallback";
  token_source: TokenSource;
}

export interface Resolution {
  selected: ResolutionSelected;
  classification_source: "command" | "name_fallback";
  alternatives: ResolutionAlternative[];
  diagnostics: ResolverDiagnostics;
}

interface Candidate {
  tool: SoloAgentTool;
  classification: Classification;
}

const isTier = (value: string): value is Tier =>
  (TIER_LABELS as readonly string[]).includes(value);

const cloneTool = (tool: SoloAgentTool): SoloAgentTool => ({
  id: tool.id,
  name: tool.name,
  command: tool.command,
  tool_type: tool.tool_type,
  enabled: tool.enabled,
});

const matchesSelector = (
  selector: PreferenceSelector,
  tool: SoloAgentTool,
): boolean => {
  if (selector.tool_type !== undefined && selector.tool_type !== tool.tool_type) {
    return false;
  }
  if (selector.tool_name !== undefined && selector.tool_name !== tool.name) {
    return false;
  }
  return true;
};

const computeRank = (
  tool: SoloAgentTool,
  preference: readonly PreferenceSelector[],
): number => {
  const idx = preference.findIndex((sel) => matchesSelector(sel, tool));
  return idx === -1 ? Number.POSITIVE_INFINITY : idx;
};

const buildMatchedTokens = (classification: Classification): MatchedToken[] => {
  const tier = classification.tier;
  if (tier === null) return [];

  if (classification.source === "command") {
    return classification.diagnostics.commandTokensSeen
      .filter((m) => m.tier === tier)
      .map((m) => ({ token: m.token, source: m.source ?? "built_in" }));
  }
  if (classification.source === "name_fallback") {
    return classification.diagnostics.nameTokensSeen
      .filter((m) => m.tier === tier)
      .map((m) => ({ token: m.token, source: "built_in" as TokenSource }));
  }
  return [];
};

export const resolveAgentTool = (
  tools: readonly SoloAgentTool[],
  tier: string,
  options: ResolverOptions = {},
): Resolution => {
  if (!isTier(tier)) {
    throw new UnsupportedTierError(tier);
  }

  const strategyName: SelectionStrategy = options.strategy ?? "random";
  if (strategyName === "custom" && options.preference === undefined) {
    throw new InvalidResolverOptionsError(
      "custom strategy requires a non-empty preference list",
    );
  }

  const rng = options.rng ?? Math.random;
  const excludeIds = new Set(options.excludeIds ?? []);
  const classifierPolicy = options.classifierPolicy;
  const preference = options.preference;

  const total_tools = tools.length;

  const enabled = tools.filter((t) => t.enabled === true);
  const enabled_count = enabled.length;

  const afterExclude: SoloAgentTool[] = [];
  let excluded_count = 0;
  for (const t of enabled) {
    if (excludeIds.has(t.id)) {
      excluded_count += 1;
    } else {
      afterExclude.push(t);
    }
  }

  const classified: Candidate[] = afterExclude.map((tool) => ({
    tool: cloneTool(tool),
    classification: classify(tool, classifierPolicy),
  }));

  let ambiguous_count = 0;
  let unclassifiable_count = 0;
  const candidates: Candidate[] = [];
  const ignored: IgnoredToolDiagnostic[] = [];

  for (const c of classified) {
    if (c.classification.ambiguous) {
      ambiguous_count += 1;
      ignored.push({
        agent_tool_id: c.tool.id,
        tool_name: c.tool.name,
        tool_type: c.tool.tool_type,
        reason: "ambiguous",
      });
      continue;
    }
    if (c.classification.tier === null) {
      unclassifiable_count += 1;
      ignored.push({
        agent_tool_id: c.tool.id,
        tool_name: c.tool.name,
        tool_type: c.tool.tool_type,
        reason: "unclassifiable",
      });
      continue;
    }
    if (c.classification.tier === tier) {
      candidates.push(c);
    } else {
      ignored.push({
        agent_tool_id: c.tool.id,
        tool_name: c.tool.name,
        tool_type: c.tool.tool_type,
        reason: "wrong_tier",
        detected_tier: c.classification.tier,
        matched_tokens: [...c.classification.matchedTokens],
        match_source: c.classification.matchSource,
      });
    }
  }

  const ranks = new Map<number, number>();
  for (const c of candidates) {
    const rank = strategyName === "custom" && preference !== undefined
      ? computeRank(c.tool, preference)
      : 0;
    ranks.set(c.tool.id, rank);
  }

  const preference_applied =
    strategyName === "custom" &&
    candidates.some((c) => Number.isFinite(ranks.get(c.tool.id)!));

  if (candidates.length === 0) {
    const diagnostics: TierUnavailableDiagnostics = {
      requested_tier: tier,
      total_tools,
      enabled_count,
      excluded_count,
      ambiguous_count,
      unclassifiable_count,
      candidates_considered: 0,
      strategy: strategyName,
      override_token_count: 0,
      preference_applied: false,
      ignored_tools: ignored,
    };
    throw new TierUnavailableError(diagnostics);
  }

  let topBucket: Candidate[];
  if (strategyName === "custom") {
    const minRank = candidates.reduce(
      (min, c) => Math.min(min, ranks.get(c.tool.id)!),
      Number.POSITIVE_INFINITY,
    );
    topBucket = candidates.filter((c) => ranks.get(c.tool.id) === minRank);
  } else {
    topBucket = candidates;
  }

  const idx = Math.floor(rng() * topBucket.length);
  const safeIdx = Math.min(Math.max(idx, 0), topBucket.length - 1);
  const selected = topBucket[safeIdx]!;

  const compareAlternatives = (a: Candidate, b: Candidate): number => {
    const rankA = ranks.get(a.tool.id)!;
    const rankB = ranks.get(b.tool.id)!;
    if (rankA !== rankB) return rankA - rankB;
    return a.tool.id - b.tool.id;
  };

  const alternatives = candidates
    .filter((c) => c.tool.id !== selected.tool.id)
    .sort(compareAlternatives)
    .map<ResolutionAlternative>((c) => ({
      agent_tool_id: c.tool.id,
      tool_name: c.tool.name,
      tool_type: c.tool.tool_type,
      classification_source: c.classification.source as
        | "command"
        | "name_fallback",
      token_source: c.classification.matchSource,
    }));

  const matchedTokens = buildMatchedTokens(selected.classification);
  const override_token_count = matchedTokens.filter(
    (m) => m.source === "override",
  ).length;

  const diagnostics: ResolverDiagnostics = {
    requested_tier: tier,
    total_tools,
    enabled_count,
    excluded_count,
    ambiguous_count,
    unclassifiable_count,
    candidates_considered: candidates.length,
    strategy: strategyName,
    override_token_count,
    preference_applied,
  };

  return {
    selected: {
      agent_tool_id: selected.tool.id,
      tool_name: selected.tool.name,
      tool_type: selected.tool.tool_type,
      command: selected.tool.command,
      token_source: selected.classification.matchSource,
      matched_tokens: matchedTokens,
    },
    classification_source: selected.classification.source as
      | "command"
      | "name_fallback",
    alternatives,
    diagnostics,
  };
};

// ---------------------------------------------------------------------------
// Preset engine (D2/D4) — additive; supersedes the tier resolver above in a
// later chunk. Selection is provider-aware random pick with `default` fallback
// and a soft `avoid_provider` preference.
// ---------------------------------------------------------------------------

export interface ResolvePresetOptions {
  /** Reuses the same seam as `resolveAgentTool`; default `Math.random`. */
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
