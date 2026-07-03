import { defineCommand } from "citty";
import { execSync } from "node:child_process";
import { existsSync } from "node:fs";
import { loadConfig, resolveConfigPath } from "../config-loader.js";
import { StdioTransport } from "../../transport/stdio.js";
import { resolveTransportCommand } from "../../transport/resolve-command.js";
import { SoloClient } from "../../solo-client.js";
import { writeJson, writeOut, green, red, dim, yellow } from "../output.js";
import { getGitSha, getVersion } from "../version-info.js";
import type { SoloConfig } from "../../config.js";

interface CheckResult {
  name: string;
  status: "ok" | "fail" | "skip";
  detail?: string;
}

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
    const version = getVersion();
    const sha = getGitSha();
    checks.push({
      name: "duo version",
      status: "ok",
      detail: sha ? `${version} (${sha})` : version,
    });

    // 2. duo config discovered + parses cleanly
    let config: SoloConfig | null = null;
    let configPath = resolveConfigPath();
    try {
      const loaded = loadConfig({ cwd });
      config = loaded.config;
      configPath = loaded.configPath;
      const base = loaded.usedDefaults ? `${configPath} (defaults — file not found)` : configPath;
      checks.push({
        name: "duo config",
        status: "ok",
        detail: base,
      });
    } catch (err) {
      checks.push({
        name: "duo config",
        status: "fail",
        detail: err instanceof Error ? err.message : String(err),
      });
    }

    // 3. Solo binary discoverable
    let resolvedCommand: string | undefined;
    if (config) {
      const configured = config.solo.transport.command;
      try {
        resolvedCommand = resolveTransportCommand(configured);
        const source = configured ? "configured" : "auto-detected";
        const resolved = which(resolvedCommand) ?? (existsSync(resolvedCommand) ? resolvedCommand : undefined);
        checks.push({
          name: "solo binary",
          status: resolved ? "ok" : "fail",
          detail: resolved
            ? `${resolved} (${source})`
            : `${resolvedCommand} not found (${source})`,
        });
      } catch (err) {
        checks.push({
          name: "solo binary",
          status: "fail",
          detail: err instanceof Error ? err.message : String(err),
        });
      }
    } else {
      checks.push({ name: "solo binary", status: "skip", detail: "no config" });
    }

    // 4-9. Solo handshake + scope resolution
    let connectErr: unknown;
    let client: SoloClient | undefined;
    if (config && resolvedCommand) {
      const transport = new StdioTransport({ ...config.solo.transport, command: resolvedCommand });
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

    // 9. list_presets returns non-empty
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
