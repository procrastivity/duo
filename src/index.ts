import { readFileSync } from "node:fs";
import { existsSync } from "node:fs";
import { parse as parseYaml } from "yaml";
import { createServer, ServerConfigError } from "./server.js";
import { loadPolicy } from "./policy.js";
import { createLogger } from "./logger.js";

const configPath = process.env.DUO_CONFIG ?? "duo.config.yaml";
let rawConfig: unknown;

try {
  rawConfig = parseYaml(readFileSync(configPath, "utf8"));
} catch (err) {
  process.stderr.write(
    `Failed to read config from ${configPath}: ${err instanceof Error ? err.message : String(err)}\n`,
  );
  process.exit(1);
}

// Load policy file
const policyPath = process.env.DUO_POLICY ?? "duo.policy.yaml";
const explicitPolicyEnv = process.env.DUO_POLICY !== undefined;

if (explicitPolicyEnv && !existsSync(policyPath)) {
  // Explicit but missing → startup error
  process.stderr.write(
    `DUO_POLICY is set to "${policyPath}" but file does not exist\n`,
  );
  process.exit(1);
}

if (existsSync(policyPath)) {
  try {
    const rawPolicy = parseYaml(readFileSync(policyPath, "utf8"));
    const policy = loadPolicy(rawPolicy);
    (rawConfig as Record<string, unknown>).policy = policy;
  } catch (err) {
    process.stderr.write(
      `Failed to parse policy from ${policyPath}: ${err instanceof Error ? err.message : String(err)}\n`,
    );
    process.exit(1);
  }
}
// If default file missing and not explicit → silent no-op (built-ins only)

const logger = createLogger();

const server = await createServer(rawConfig, logger).catch((err: unknown) => {
  const message =
    err instanceof ServerConfigError
      ? err.message
      : `Unexpected error: ${err instanceof Error ? err.message : String(err)}`;
  process.stderr.write(`${message}\n`);
  process.exit(1);
});

await server.start().catch((err: unknown) => {
  process.stderr.write(
    `Server failed to start: ${err instanceof Error ? err.message : String(err)}\n`,
  );
  process.exit(1);
});
