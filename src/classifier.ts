import type { Policy } from "./types/policy.js";
import type { SoloAgentTool } from "./types/solo.js";

export type Tier = "small" | "medium" | "large";
export type ClassificationSource = "command" | "name_fallback" | "none";
export type TokenSource = "built_in" | "override";

export interface TokenMatch {
  tier: Tier;
  token: string;
  source?: TokenSource;
}

export interface ClassificationDiagnostics {
  commandTokensSeen: TokenMatch[];
  nameTokensSeen: TokenMatch[];
}

export interface Classification {
  tier: Tier | null;
  source: ClassificationSource;
  matchedTokens: string[];
  ambiguous: boolean;
  matchSource: TokenSource;
  diagnostics: ClassificationDiagnostics;
}

export interface ClassifierTokenPolicy {
  command: Readonly<Record<Tier, ReadonlyArray<{ token: string; source: TokenSource }>>>;
  name: Readonly<Record<Tier, readonly string[]>>;
}

export const COMMAND_TOKENS: Readonly<Record<Tier, readonly string[]>> = {
  small:  ["haiku", "mini", "flash", "fast", "cheap", "small"],
  medium: ["sonnet", "standard", "medium", "default", "gpt-5.2", "gpt-5.3-codex", "gpt-5.4"],
  large:  ["opus", "flagship", "max", "large", "gpt-5.5"],
};

export const NAME_TOKENS: Readonly<Record<Tier, readonly string[]>> = {
  small:  ["haiku", "mini", "flash", "fast", "cheap", "small"],
  medium: ["sonnet", "standard", "medium", "default"],
  large:  ["opus", "flagship", "pro", "max", "large"],
};

export const defaultPolicy = (): ClassifierTokenPolicy => {
  return {
    command: {
      small: COMMAND_TOKENS.small.map((token) => ({ token, source: "built_in" })),
      medium: COMMAND_TOKENS.medium.map((token) => ({ token, source: "built_in" })),
      large: COMMAND_TOKENS.large.map((token) => ({ token, source: "built_in" })),
    },
    name: NAME_TOKENS,
  };
};

export const buildClassifierPolicy = (
  policy: Policy,
): ClassifierTokenPolicy => {
  const builtInPolicy = defaultPolicy();
  const overrides = policy.command_tokens || {};
  const TIERS: readonly Tier[] = ["small", "medium", "large"];

  const commandPolicy: Record<Tier, Array<{ token: string; source: TokenSource }>> = {
    small: [],
    medium: [],
    large: [],
  };

  for (const tier of TIERS) {
    const override = overrides[tier];
    if (!override) {
      // No override: use built-in tokens
      commandPolicy[tier] = [...builtInPolicy.command[tier]];
    } else if (override.mode === "replace") {
      // Replace mode: use only override tokens
      commandPolicy[tier] = override.tokens.map((token) => ({
        token,
        source: "override" as const,
      }));
    } else {
      // Extend mode: built-in + override, dedup case-insensitive, first-occurrence wins
      const seen = new Set<string>();
      const result: Array<{ token: string; source: TokenSource }> = [];

      // Add built-in tokens first
      for (const tokenObj of builtInPolicy.command[tier]) {
        const lower = tokenObj.token.toLowerCase();
        if (!seen.has(lower)) {
          seen.add(lower);
          result.push(tokenObj);
        }
      }

      // Add override tokens second
      for (const token of override.tokens) {
        const lower = token.toLowerCase();
        if (!seen.has(lower)) {
          seen.add(lower);
          result.push({ token, source: "override" });
        }
      }

      commandPolicy[tier] = result;
    }
  }

  return {
    command: commandPolicy,
    name: builtInPolicy.name,
  };
};

const TIERS: readonly Tier[] = ["small", "medium", "large"];

const escapeRegex = (s: string): string =>
  s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const findCommandTokens = (
  text: string,
  policy: Readonly<Record<Tier, ReadonlyArray<{ token: string; source: TokenSource }>>>,
): TokenMatch[] => {
  const lower = text.toLowerCase();
  const seen: TokenMatch[] = [];
  for (const tier of TIERS) {
    for (const tokenObj of policy[tier]) {
      const re = new RegExp(`\\b${escapeRegex(tokenObj.token)}\\b`);
      if (re.test(lower)) {
        seen.push({ tier, token: tokenObj.token, source: tokenObj.source });
      }
    }
  }
  return seen;
};

const findNameTokens = (
  text: string,
  policy: Readonly<Record<Tier, readonly string[]>>,
): TokenMatch[] => {
  const lower = text.toLowerCase();
  const seen: TokenMatch[] = [];
  for (const tier of TIERS) {
    for (const token of policy[tier]) {
      const re = new RegExp(`\\b${escapeRegex(token)}\\b`);
      if (re.test(lower)) {
        seen.push({ tier, token });
      }
    }
  }
  return seen;
};

const uniqueTiers = (matches: readonly TokenMatch[]): Tier[] => {
  const set = new Set<Tier>();
  for (const m of matches) set.add(m.tier);
  return [...set];
};

const getFirstMatchSource = (
  matches: readonly TokenMatch[],
): TokenSource => {
  if (matches.length > 0 && matches[0]!.source !== undefined) {
    return matches[0]!.source;
  }
  return "built_in";
};

export const classify = (
  tool: SoloAgentTool,
  policy?: ClassifierTokenPolicy,
): Classification => {
  const effectivePolicy = policy || defaultPolicy();
  const commandTokensSeen = findCommandTokens(tool.command, effectivePolicy.command);
  const nameTokensSeen = findNameTokens(tool.name, effectivePolicy.name);
  const diagnostics: ClassificationDiagnostics = {
    commandTokensSeen,
    nameTokensSeen,
  };

  const cmdTiers = uniqueTiers(commandTokensSeen);

  if (cmdTiers.length === 1) {
    const tier = cmdTiers[0]!;
    const tierMatches = commandTokensSeen.filter((m) => m.tier === tier);
    return {
      tier,
      source: "command",
      matchedTokens: tierMatches.map((m) => m.token),
      ambiguous: false,
      matchSource: getFirstMatchSource(tierMatches),
      diagnostics,
    };
  }

  if (cmdTiers.length > 1) {
    return {
      tier: null,
      source: "none",
      matchedTokens: [],
      ambiguous: true,
      matchSource: "built_in",
      diagnostics,
    };
  }

  const nameTiers = uniqueTiers(nameTokensSeen);

  if (nameTiers.length === 1) {
    const tier = nameTiers[0]!;
    const tierMatches = nameTokensSeen.filter((m) => m.tier === tier);
    return {
      tier,
      source: "name_fallback",
      matchedTokens: tierMatches.map((m) => m.token),
      ambiguous: false,
      matchSource: "built_in",
      diagnostics,
    };
  }

  return {
    tier: null,
    source: "none",
    matchedTokens: [],
    ambiguous: false,
    matchSource: "built_in",
    diagnostics,
  };
};
