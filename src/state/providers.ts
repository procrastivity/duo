import { mkdirSync, readdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  assertValidProviderLabel,
  isValidProviderLabel,
  resolveProviderStateDir,
} from "./paths.js";

/**
 * In-process counter for unique temp filenames. Combined with `process.pid` this
 * yields a collision-free temp name without `Date.now()`/random (which are
 * intentionally avoided). This is NOT a cache of file contents or directory
 * listings — reads always hit the filesystem fresh.
 */
let tempCounter = 0;

/**
 * Read a provider's enabled-state fresh from disk on every call.
 *
 * Semantics (BRIEF §Design.2): file content `0` (after trimming) ⇒ disabled;
 * the file being absent/unreadable, or holding any other content, ⇒ enabled
 * (opt-out default). An unparseable/never-written label reads as enabled rather
 * than throwing.
 */
export const isProviderEnabled = (provider: string): boolean => {
  const file = join(resolveProviderStateDir(), provider);
  let contents: string;
  try {
    contents = readFileSync(file, "utf8");
  } catch {
    // Absent or unreadable ⇒ enabled (opt-out default).
    return true;
  }
  return contents.trim() !== "0";
};

/**
 * Set a provider's enabled-state atomically and lock-free.
 *
 * Enable writes `1`, disable writes `0` (enable does NOT delete the file — that
 * keeps the provider enumerable by `listProviders`). The label is validated
 * before any filesystem access. The write goes to a sibling temp file in the
 * same directory, then `renameSync` atomically replaces the target (rename is
 * atomic within one filesystem, so no lock is needed).
 */
export const setProviderEnabled = (provider: string, enabled: boolean): void => {
  assertValidProviderLabel(provider);
  const dir = resolveProviderStateDir();
  mkdirSync(dir, { recursive: true });
  const target = join(dir, provider);
  const tmp = join(dir, `${provider}.${process.pid}.${tempCounter++}.tmp`);
  writeFileSync(tmp, enabled ? "1" : "0");
  renameSync(tmp, target);
};

/**
 * Enumerate the provider state directory only, fresh on every call.
 *
 * Returns `{ provider, enabled }` for every file present, sorted by provider for
 * deterministic output; a missing directory yields `[]`. Invalid labels and
 * transient temp files are ignored so only real provider state files are
 * surfaced. Preset-derived providers are intentionally NOT unioned in here.
 */
export const listProviders = (): { provider: string; enabled: boolean }[] => {
  let entries: string[];
  try {
    entries = readdirSync(resolveProviderStateDir());
  } catch {
    return [];
  }
  return entries
    .filter((provider) => isValidProviderLabel(provider) && !provider.endsWith(".tmp"))
    .map((provider) => ({ provider, enabled: isProviderEnabled(provider) }))
    .sort((a, b) => (a.provider < b.provider ? -1 : a.provider > b.provider ? 1 : 0));
};
