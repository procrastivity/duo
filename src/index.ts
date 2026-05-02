import { readFileSync } from "node:fs";
import { parse as parseYaml } from "yaml";
import { createServer, ServerConfigError } from "./server.js";

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

const server = await createServer(rawConfig).catch((err: unknown) => {
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
