import { homedir } from "node:os";
import { join } from "node:path";

/**
 * Resolve the Duo provider-state directory.
 *
 * Mirrors `src/cli/config-loader.ts:resolveConfigPath()`'s XDG handling:
 *   - `XDG_STATE_HOME` set → `$XDG_STATE_HOME/duo/providers`
 *   - otherwise            → `~/.local/state/duo/providers` (via `homedir()`)
 *
 * No `DUO_STATE_HOME` override is added — `XDG_STATE_HOME` covers both real use
 * and test isolation, matching how `resolveConfigPath` tests drive
 * `XDG_CONFIG_HOME`.
 */
export const resolveProviderStateDir = (): string => {
  const xdgStateHome = process.env.XDG_STATE_HOME;
  if (xdgStateHome) {
    return join(xdgStateHome, "duo", "providers");
  }
  return join(homedir(), ".local", "state", "duo", "providers");
};

const PROVIDER_LABEL_PATTERN = /^[A-Za-z0-9._-]+$/;

/**
 * A provider label maps directly to a filename under the state directory, so it
 * must be a single safe path segment. Accepts `^[A-Za-z0-9._-]+$`; rejects the
 * empty string, `.`, `..`, anything containing a path separator, and anything
 * with characters outside the charset. This blocks path traversal / separators
 * / empty labels before any filesystem access.
 */
export const isValidProviderLabel = (label: string): boolean => {
  if (label === "." || label === "..") return false;
  return PROVIDER_LABEL_PATTERN.test(label);
};

/**
 * Throw a clear error if `label` is not a valid provider label. Callers use this
 * to reject unsafe labels before touching the filesystem.
 */
export const assertValidProviderLabel = (label: string): void => {
  if (!isValidProviderLabel(label)) {
    throw new Error(
      `Invalid provider label ${JSON.stringify(label)}. Provider labels must ` +
        `match ^[A-Za-z0-9._-]+$ and cannot be "", ".", "..", or contain a ` +
        `path separator.`,
    );
  }
};
