import { existsSync } from "node:fs";

/**
 * Known Solo MCP binary paths, checked in order when no explicit command is configured.
 */
export const KNOWN_SOLO_PATHS: readonly string[] = [
  "/Applications/Solo.app/Contents/MacOS/mcp",
];

/**
 * Resolve the Solo MCP transport command path.
 *
 * Resolution order:
 * 1. `configured` — if provided, use it as-is (explicit config takes precedence)
 * 2. Auto-detect — check each known path in order; return the first that exists
 * 3. Throw a descriptive error if neither yields a path
 */
export const resolveTransportCommand = (configured?: string): string => {
  if (configured) {
    return configured;
  }

  for (const candidate of KNOWN_SOLO_PATHS) {
    if (existsSync(candidate)) {
      return candidate;
    }
  }

  const searchedPaths = KNOWN_SOLO_PATHS.join(", ");
  throw new Error(
    `Solo MCP binary not found. ` +
      `Searched: ${searchedPaths}. ` +
      `Set solo.transport.command in your config or install Solo at a known location.`,
  );
};
