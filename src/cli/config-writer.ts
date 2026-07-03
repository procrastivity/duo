import { randomBytes } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { parse as parseYaml, stringify as stringifyYaml } from "yaml";
import { parseConfig, type SoloConfig } from "../config.js";
import { DEFAULT_RAW_CONFIG, resolveConfigPath } from "./config-loader.js";

/**
 * Read the RAW `config.yaml` at `resolveConfigPath()` as a mutable plain object,
 * or seed a fresh object from {@link DEFAULT_RAW_CONFIG} when the file is absent
 * (or present-but-empty).
 *
 * This deliberately bypasses `loadConfig` (and thus `parseConfig`) so that
 * schema defaults injected during parsing are never written back to disk.
 * Callers that intend to write MUST start from this raw view.
 */
export const readRawConfig = (): Record<string, unknown> => {
  const configPath = resolveConfigPath();

  if (!existsSync(configPath)) {
    return structuredClone(DEFAULT_RAW_CONFIG) as Record<string, unknown>;
  }

  let fileContents: string;
  try {
    fileContents = readFileSync(configPath, "utf8");
  } catch (err) {
    throw new Error(
      `Failed to read config from ${configPath}: ${err instanceof Error ? err.message : String(err)}`,
    );
  }

  let parsed: unknown;
  try {
    parsed = parseYaml(fileContents);
  } catch (err) {
    throw new Error(
      `Failed to parse config at ${configPath}: ${err instanceof Error ? err.message : String(err)}`,
    );
  }

  // An empty / whitespace-only / comment-only file parses to null|undefined,
  // and an empty mapping to {}. Nothing to mutate — seed from defaults so the
  // caller can add presets to a well-formed base.
  if (
    parsed === null ||
    parsed === undefined ||
    (typeof parsed === "object" &&
      !Array.isArray(parsed) &&
      Object.keys(parsed as object).length === 0)
  ) {
    return structuredClone(DEFAULT_RAW_CONFIG) as Record<string, unknown>;
  }

  if (typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(
      `Config at ${configPath} must be a mapping, got ${Array.isArray(parsed) ? "array" : typeof parsed}.`,
    );
  }

  return parsed as Record<string, unknown>;
};

/**
 * Validate `next` against `soloConfigSchema` and, only if it passes, persist it
 * to `resolveConfigPath()`.
 *
 * - Validation happens BEFORE any filesystem mutation: an invalid object throws
 *   and nothing is written (or partially written).
 * - The parent directory is created if missing.
 * - The write is atomic: a temp sibling file is written then `rename`d over the
 *   destination, so a reader never observes a half-written config.
 *
 * The RAW `next` object is what gets serialized (not the schema-parsed result),
 * so schema defaults (e.g. `solo.transport.args: []`) are not injected into the
 * on-disk file.
 *
 * @returns the validated {@link SoloConfig}.
 */
export const writeConfig = (next: unknown): SoloConfig => {
  // Validate BEFORE persisting — throws on an invalid config, writing nothing.
  const validated = parseConfig(next);

  const configPath = resolveConfigPath();
  const dir = dirname(configPath);
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }

  const serialized = stringifyYaml(next);
  const tmpPath = `${configPath}.${randomBytes(6).toString("hex")}.tmp`;

  try {
    writeFileSync(tmpPath, serialized, "utf8");
    renameSync(tmpPath, configPath);
  } catch (err) {
    // Best-effort cleanup of the temp file on failure; ignore secondary errors.
    try {
      if (existsSync(tmpPath)) {
        writeFileSync(tmpPath, "");
      }
    } catch {
      // ignore
    }
    throw err;
  }

  return validated;
};

/**
 * Generate a short, stable, base36 definition id (8 chars) via `node:crypto`
 * `randomBytes` (no new dependency). Regenerates on collision against
 * `existingIds` so ids are globally unique across all presets (D2), letting
 * `preset remove <id>` target a single definition without a preset name and
 * without reindexing survivors.
 */
export const generateDefinitionId = (existingIds: Iterable<string> = []): string => {
  const taken = new Set(existingIds);

  // 8 random bytes (64 bits) reliably yields >= 8 base36 chars; slice to 8.
  for (;;) {
    const candidate = BigInt(`0x${randomBytes(8).toString("hex")}`)
      .toString(36)
      .padStart(8, "0")
      .slice(0, 8);
    if (!taken.has(candidate)) {
      return candidate;
    }
  }
};
