import type { SoloAgentTool } from "../types/solo.js";

export const enabledRuntimes: SoloAgentTool[] = [
  { id: 1, name: "opencode-ghc-haiku",  command: "opencode --model haiku",   tool_type: "opencode", enabled: true },
  { id: 2, name: "opencode-ghc-sonnet", command: "opencode --model sonnet",  tool_type: "opencode", enabled: true },
  { id: 3, name: "codex-fast",          command: "codex --profile fast",     tool_type: "codex",    enabled: true },
  { id: 4, name: "codex-standard",      command: "codex --profile standard", tool_type: "codex",    enabled: true },
  { id: 5, name: "codex-flagship",      command: "codex --profile flagship", tool_type: "codex",    enabled: true },
];

export const disabledVariants: SoloAgentTool[] = enabledRuntimes.map((t) => ({
  ...t,
  enabled: false,
}));

// Name "mini-helper" contains "mini" (small token) but command specifies "opus" (large token).
// Classifier must pick large from command and must not consult the name.
export const misleadingNameAccurateCommand: SoloAgentTool = {
  id: 10,
  name: "mini-helper",
  command: "special-runner --backend opus --config prod-settings",
  tool_type: "special",
  enabled: true,
};

// Name "sonnet-runner" contains "sonnet" (medium token) but command has no recognized token.
// Classifier falls back to name, yielding medium with source="name_fallback".
export const accurateNameMisleadingCommand: SoloAgentTool = {
  id: 11,
  name: "sonnet-runner",
  command: "custom-runner --config production --timeout 30",
  tool_type: "custom",
  enabled: true,
};

// Command contains both "haiku" (small) and "opus" (large): two tiers → ambiguous.
// Classifier reports tier=null, ambiguous=true, source="none" and does not consult name.
export const ambiguousCommand: SoloAgentTool = {
  id: 12,
  name: "multi-model-selector",
  command: "model-switcher --primary haiku --fallback opus",
  tool_type: "experimental",
  enabled: true,
};

// Command has no recognized tokens; name has no recognized tokens either.
// Classifier yields tier=null, source="none" (unclassifiable).
export const unknownCommand: SoloAgentTool = {
  id: 13,
  name: "custom-runner",
  command: "python scripts/run_agent.py --config agent.json",
  tool_type: "custom",
  enabled: true,
};

// Combines enabled runtimes (all three tiers covered), a pair of disabled tools
// (to verify they are filtered before classification), and all edge-case singles.
// Used by the list_agent_tiers integration test to exercise the full pipeline.
export const mixedRealistic: SoloAgentTool[] = [
  ...enabledRuntimes,
  { id: 21, name: "opencode-ghc-haiku-v1",     command: "opencode --model haiku --version 1",    tool_type: "opencode", enabled: false },
  { id: 22, name: "codex-standard-preview",    command: "codex --profile standard --preview",    tool_type: "codex",    enabled: false },
  misleadingNameAccurateCommand,
  accurateNameMisleadingCommand,
  ambiguousCommand,
  unknownCommand,
];
