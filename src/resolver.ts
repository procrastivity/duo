import { classify, type Classification, type Tier } from "./classifier.js";
import {
  TIER_LABELS,
  TierUnavailableError,
  UnsupportedTierError,
  type IgnoredToolDiagnostic,
  type ResolverDiagnostics,
  type TierUnavailableDiagnostics,
} from "./errors.js";
import type { SoloAgentTool } from "./types/solo.js";

export type SelectionStrategy = "random";

export interface ResolverOptions {
  strategy?: SelectionStrategy;
  excludeIds?: number[];
  rng?: () => number;
}

export interface ResolutionSelected {
  agent_tool_id: number;
  tool_name: string;
  tool_type: string;
  command: string;
}

export interface ResolutionAlternative {
  agent_tool_id: number;
  tool_name: string;
  tool_type: string;
  classification_source: "command" | "name_fallback";
}

export interface Resolution {
  selected: ResolutionSelected;
  classification_source: "command" | "name_fallback";
  matched_tokens: string[];
  alternatives: ResolutionAlternative[];
  diagnostics: ResolverDiagnostics;
}

interface Candidate {
  tool: SoloAgentTool;
  classification: Classification;
}

interface SelectionStrategyImpl {
  select(candidates: readonly Candidate[], rng: () => number): Candidate;
}

const randomStrategy: SelectionStrategyImpl = {
  select(candidates, rng) {
    const idx = Math.floor(rng() * candidates.length);
    const safeIdx = Math.min(Math.max(idx, 0), candidates.length - 1);
    return candidates[safeIdx]!;
  },
};

const STRATEGIES: Readonly<Record<SelectionStrategy, SelectionStrategyImpl>> = {
  random: randomStrategy,
};

const isTier = (value: string): value is Tier =>
  (TIER_LABELS as readonly string[]).includes(value);

const cloneTool = (tool: SoloAgentTool): SoloAgentTool => ({
  id: tool.id,
  name: tool.name,
  command: tool.command,
  tool_type: tool.tool_type,
  enabled: tool.enabled,
});

export const resolveAgentTool = (
  tools: readonly SoloAgentTool[],
  tier: string,
  options: ResolverOptions = {},
): Resolution => {
  if (!isTier(tier)) {
    throw new UnsupportedTierError(tier);
  }

  const strategyName: SelectionStrategy = options.strategy ?? "random";
  const strategy = STRATEGIES[strategyName];
  const rng = options.rng ?? Math.random;
  const excludeIds = new Set(options.excludeIds ?? []);

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
    classification: classify(tool),
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
      });
    }
  }

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
      ignored_tools: ignored,
    };
    throw new TierUnavailableError(diagnostics);
  }

  const selected = strategy.select(candidates, rng);

  const alternatives = candidates
    .filter((c) => c.tool.id !== selected.tool.id)
    .sort((a, b) => a.tool.id - b.tool.id)
    .map<ResolutionAlternative>((c) => ({
      agent_tool_id: c.tool.id,
      tool_name: c.tool.name,
      tool_type: c.tool.tool_type,
      classification_source: c.classification.source as
        | "command"
        | "name_fallback",
    }));

  const diagnostics: ResolverDiagnostics = {
    requested_tier: tier,
    total_tools,
    enabled_count,
    excluded_count,
    ambiguous_count,
    unclassifiable_count,
    candidates_considered: candidates.length,
    strategy: strategyName,
  };

  return {
    selected: {
      agent_tool_id: selected.tool.id,
      tool_name: selected.tool.name,
      tool_type: selected.tool.tool_type,
      command: selected.tool.command,
    },
    classification_source: selected.classification.source as
      | "command"
      | "name_fallback",
    matched_tokens: [...selected.classification.matchedTokens],
    alternatives,
    diagnostics,
  };
};
