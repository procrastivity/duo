import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { parse as parseYaml } from "yaml";
import { parseConfig, type SoloConfig } from "../config.js";
import { loadPolicy } from "../policy.js";

export interface LoadedConfig {
  config: SoloConfig;
  configPath: string;
  policyPath: string | null;
}

export interface LoadConfigOptions {
  cwd?: string;
}

export const resolveConfigPath = (cwd: string = process.cwd()): string =>
  process.env.DUO_CONFIG ?? resolve(cwd, "duo.config.yaml");

export const resolvePolicyPath = (cwd: string = process.cwd()): string =>
  process.env.DUO_POLICY ?? resolve(cwd, "duo.policy.yaml");

export const loadConfig = (opts: LoadConfigOptions = {}): LoadedConfig => {
  const cwd = opts.cwd ?? process.cwd();
  const configPath = resolveConfigPath(cwd);
  let raw: unknown;
  try {
    raw = parseYaml(readFileSync(configPath, "utf8"));
  } catch (err) {
    throw new Error(
      `Failed to read config from ${configPath}: ${err instanceof Error ? err.message : String(err)}`,
    );
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
  return { config, configPath, policyPath: usedPolicyPath };
};
