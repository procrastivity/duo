import { defineCommand } from "citty";
import { execSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadConfig, resolveConfigPath } from "../config-loader.js";
import { StdioTransport } from "../../transport/stdio.js";
import { SoloClient } from "../../solo-client.js";
import { writeJson, writeOut, writeErr, green, red, dim, yellow } from "../output.js";
import type { SoloConfig } from "../../config.js";

interface CheckResult {
  name: string;
  status: "ok" | "fail" | "skip";
  detail?: string;
}

const findVersion = (): string => {
  const here = dirname(fileURLToPath(import.meta.url));
  for (const path of [
    resolve(here, "../../../package.json"),
    resolve(here, "../../package.json"),
    resolve(here, "../package.json"),
  ]) {
    try {
      const pkg = JSON.parse(readFileSync(path, "utf8"));
      if (pkg.name && pkg.version) return pkg.version;
    } catch {
      // try next
    }
  }
  return "unknown";
};

const findGitSha = (): string | undefined => {
  try {
    return execSync("git rev-parse --short HEAD", { stdio: ["ignore", "pipe", "ignore"] })
      .toString()
      .trim();
  } catch {
    return undefined;
  }
};

const symbol = (status: CheckResult["status"], opts: { noColor?: boolean }): string => {
  if (status === "ok") return green("✓", opts);
  if (status === "fail") return red("✗", opts);
  return yellow("—", opts);
};

const which = (cmd: string): string | undefined => {
  try {
    return execSync(`command -v ${cmd}`, { stdio: ["ignore", "pipe", "ignore"] })
      .toString()
      .trim();
  } catch {
    return undefined;
  }
};

export const doctorCommand = defineCommand({
  meta: { name: "doctor", description: "Run health checks for the Duo + Solo setup" },
  args: {
    cwd: { type: "string" },
    json: { type: "boolean" },
    "no-color": { type: "boolean" },
  },
  async run({ args }) {
    const cwd = args.cwd ?? process.cwd();
    const noColor = Boolean(args["no-color"]);
    const checks: CheckResult[] = [];

    // 1. Duo binary version + git sha
    const version = findVersion();
    const sha = findGitSha();
    checks.push({
      name: "duo version",
      status: "ok",
      detail: sha ? `${version} (${sha})` : version,
    });

    // 2. duo.config.yaml discovered + parses cleanly
    let config: SoloConfig | null = null;
    let configPath = resolveConfigPath(cwd);
    let policyPath: string | null = null;
    try {
      const loaded = loadConfig({ cwd });
      config = loaded.config;
      configPath = loaded.configPath;
      policyPath = loaded.policyPath;
      checks.push({
        name: "duo.config.yaml",
        status: "ok",
        detail: policyPath ? `${configPath} (+ policy: ${policyPath})` : configPath,
      });
    } catch (err) {
      checks.push({
        name: "duo.config.yaml",
        status: "fail",
        detail: err instanceof Error ? err.message : String(err),
      });
    }

    // 3. Solo binary discoverable
    if (config) {
      const cmd = config.solo.transport.command;
      const resolved = which(cmd) ?? (existsSync(cmd) ? cmd : undefined);
      checks.push({
        name: "solo binary",
        status: resolved ? "ok" : "fail",
        detail: resolved ?? `${cmd} not found in PATH`,
      });
    } else {
      checks.push({ name: "solo binary", status: "skip", detail: "no config" });
    }

    // 4-9. Solo handshake + scope resolution
    let connectErr: unknown;
    let client: SoloClient | undefined;
    if (config) {
      const transport = new StdioTransport(config.solo.transport);
      const log = {
        info: () => {},
        warn: () => {},
      };
      client = new SoloClient(transport, { cwd, env: process.env, logger: log });
      try {
        await client.connect();
        checks.push({ name: "solo handshake", status: "ok" });
      } catch (err) {
        connectErr = err;
        checks.push({
          name: "solo handshake",
          status: "fail",
          detail: err instanceof Error ? err.message : String(err),
        });
      }
    } else {
      checks.push({ name: "solo handshake", status: "skip" });
    }

    // 5. SOLO_PROJECT_ID env
    const envProject = process.env.SOLO_PROJECT_ID;
    checks.push({
      name: "SOLO_PROJECT_ID",
      status: envProject ? "ok" : "skip",
      detail: envProject ?? "(unset)",
    });

    // 6. SOLO_PROCESS_ID env
    const envProcess = process.env.SOLO_PROCESS_ID;
    checks.push({
      name: "SOLO_PROCESS_ID",
      status: envProcess ? "ok" : "skip",
      detail: envProcess ?? "(unset)",
    });

    // 7. cwd → project resolution
    if (client && !connectErr) {
      const pid = client.projectId;
      checks.push({
        name: "project scope resolved",
        status: pid !== undefined ? "ok" : "fail",
        detail:
          pid !== undefined
            ? `project_id=${pid} (cwd=${cwd})`
            : `no Solo project matches ${cwd}`,
      });
    } else {
      checks.push({ name: "project scope resolved", status: "skip" });
    }

    // 8. bind_session_process outcome
    if (client && !connectErr) {
      if (envProcess === undefined) {
        checks.push({
          name: "bind_session_process",
          status: "skip",
          detail: "SOLO_PROCESS_ID unset",
        });
      } else if (client.processId !== undefined) {
        checks.push({
          name: "bind_session_process",
          status: "ok",
          detail: `bound process_id=${client.processId}`,
        });
      } else {
        checks.push({
          name: "bind_session_process",
          status: "fail",
          detail: "binding failed (see connect logs)",
        });
      }
    } else {
      checks.push({ name: "bind_session_process", status: "skip" });
    }

    // 9. list_agent_tiers returns non-empty
    if (client && !connectErr) {
      try {
        const tools = await client.listAgentTools();
        const enabled = tools.filter((t) => t.enabled);
        checks.push({
          name: "agent tools available",
          status: enabled.length > 0 ? "ok" : "fail",
          detail: `${enabled.length} enabled / ${tools.length} total`,
        });
      } catch (err) {
        checks.push({
          name: "agent tools available",
          status: "fail",
          detail: err instanceof Error ? err.message : String(err),
        });
      }
    } else {
      checks.push({ name: "agent tools available", status: "skip" });
    }

    if (client) {
      try {
        await client.disconnect();
      } catch {
        // ignore
      }
    }

    if (args.json) {
      writeJson({ checks });
    } else {
      const nameWidth = Math.max(...checks.map((c) => c.name.length));
      for (const c of checks) {
        const sym = symbol(c.status, { noColor });
        const detail = c.detail ? dim(`  ${c.detail}`, { noColor }) : "";
        writeOut(`${sym} ${c.name.padEnd(nameWidth)}${detail}`);
      }
    }

    const failed = checks.some((c) => c.status === "fail");
    if (failed) process.exit(1);
  },
});
