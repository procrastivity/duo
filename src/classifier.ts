import type { SoloAgentTool } from "./types/solo.js";

export type Tier = "small" | "medium" | "large";
export type ClassificationSource = "command" | "name_fallback" | "none";

export interface TokenMatch {
  tier: Tier;
  token: string;
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
  diagnostics: ClassificationDiagnostics;
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

const TIERS: readonly Tier[] = ["small", "medium", "large"];

const escapeRegex = (s: string): string =>
  s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const findTokens = (
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

export const classify = (tool: SoloAgentTool): Classification => {
  const commandTokensSeen = findTokens(tool.command, COMMAND_TOKENS);
  const nameTokensSeen = findTokens(tool.name, NAME_TOKENS);
  const diagnostics: ClassificationDiagnostics = {
    commandTokensSeen,
    nameTokensSeen,
  };

  const cmdTiers = uniqueTiers(commandTokensSeen);

  if (cmdTiers.length === 1) {
    const tier = cmdTiers[0]!;
    return {
      tier,
      source: "command",
      matchedTokens: commandTokensSeen
        .filter((m) => m.tier === tier)
        .map((m) => m.token),
      ambiguous: false,
      diagnostics,
    };
  }

  if (cmdTiers.length > 1) {
    return {
      tier: null,
      source: "none",
      matchedTokens: [],
      ambiguous: true,
      diagnostics,
    };
  }

  const nameTiers = uniqueTiers(nameTokensSeen);

  if (nameTiers.length === 1) {
    const tier = nameTiers[0]!;
    return {
      tier,
      source: "name_fallback",
      matchedTokens: nameTokensSeen
        .filter((m) => m.tier === tier)
        .map((m) => m.token),
      ambiguous: false,
      diagnostics,
    };
  }

  return {
    tier: null,
    source: "none",
    matchedTokens: [],
    ambiguous: false,
    diagnostics,
  };
};
