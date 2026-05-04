import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { parse as parseYaml } from "yaml";
import { parseConfig, type SoloConfig } from "../config.js";
import { loadPolicy } from "../policy.js";

export interface LoadedConfig {
  config: SoloConfig;
  configPath: string;
  policyPath: string | null;
  usedDefaults: boolean;
}

const DEFAULT_RAW_CONFIG = {
  solo: { transport: { type: "stdio" } },
} as const;

export interface LoadConfigOptions {
  cwd?: string;
}

/**
 * Resolve the Duo config file path.
 *
 * Resolution order:
 * 1. DUO_CONFIG env var — verbatim path (highest priority, existing behaviour)
 * 2. $XDG_CONFIG_HOME/duo/config.yaml — if XDG_CONFIG_HOME is set
 * 3. ~/.config/duo/config.yaml — unconditional XDG default fallback
 *
 * Note: cwd-relative lookup has been intentionally removed. Users previously
 * relying on `duo.config.yaml` in cwd should move it to ~/.config/duo/config.yaml
 * or set the DUO_CONFIG environment variable.
 */
export const resolveConfigPath = (): string => {
  if (process.env.DUO_CONFIG) {
    return process.env.DUO_CONFIG;
  }

  const xdgConfigHome = process.env.XDG_CONFIG_HOME;
  if (xdgConfigHome) {
    return join(xdgConfigHome, "duo", "config.yaml");
  }

  return join(homedir(), ".config", "duo", "config.yaml");
};

export const resolvePolicyPath = (cwd: string = process.cwd()): string =>
  process.env.DUO_POLICY ?? resolve(cwd, "duo.policy.yaml");

export const loadConfig = (opts: LoadConfigOptions = {}): LoadedConfig => {
  const cwd = opts.cwd ?? process.cwd();
  const configPath = resolveConfigPath();
  let raw: unknown;
  let usedDefaults = false;
  if (!existsSync(configPath)) {
    raw = structuredClone(DEFAULT_RAW_CONFIG) as Record<string, unknown>;
    usedDefaults = true;
  } else {
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

    const isEmptyObject =
      parsed !== null &&
      typeof parsed === "object" &&
      !Array.isArray(parsed) &&
      Object.keys(parsed as object).length === 0;
    if (parsed === null || parsed === undefined || isEmptyObject) {
      throw new Error(
        `Config at ${configPath} is empty. Minimum required keys:\n  solo:\n    transport:\n      type: stdio\nOr delete the file to use defaults.`,
      );
    }

    raw = parsed;
  }

  const policyPath = resolvePolicyPath(cwd);
  const explicitPolicyEnv = process.env.DUO_POLICY !== undefined;
  let usedPolicyPath: string | null = null;

  if (explicitPolicyEnv && !existsSync(policyPath)) {
    throw new Error(`DUO_POLICY is set to "${policyPath}" but file does not exist`);
  }

  if (existsSync(policyPath)) {
    try {
      const rawPolicy = parseYaml(readFileSync(policyPath, "utf8"));
      const policy = loadPolicy(rawPolicy);
      (raw as Record<string, unknown>).policy = policy;
      usedPolicyPath = policyPath;
    } catch (err) {
      throw new Error(
        `Failed to parse policy from ${policyPath}: ${err instanceof Error ? err.message : String(err)}`,
      );
    }
  }

  const config = parseConfig(raw);
  return { config, configPath, policyPath: usedPolicyPath, usedDefaults };
};
