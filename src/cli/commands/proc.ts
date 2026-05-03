import { defineCommand } from "citty";
import { connectSolo, handleSoloError, EXIT_USER_ERROR } from "../connect.js";
import { SoloClient } from "../../solo-client.js";
import {
  printResult,
  printObject,
  writeErr,
  writeJson,
  writeOut,
} from "../output.js";

const parseProcessRef = (input: string): { process_id?: number; process_name?: string } => {
  if (/^\d+$/.test(input)) return { process_id: Number(input) };
  return { process_name: input };
};

interface ProcessRow {
  id: number;
  name: string;
  status: string;
  command: string;
  pid?: number;
  uptime_seconds?: number;
}

const lsCommand = defineCommand({
  meta: { name: "ls", description: "List processes in the current project" },
  args: {
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const procs = await client.callTool<ProcessRow[]>("list_processes", {});
      printResult(
        procs ?? [],
        [
          { header: "ID", get: (p) => String(p.id) },
          { header: "NAME", get: (p) => p.name, truncate: 40 },
          { header: "STATUS", get: (p) => p.status },
          { header: "COMMAND", get: (p) => p.command, truncate: 60 },
        ],
        { json: args.json, quiet: args.quiet, quietField: (p) => String(p.id) },
      );
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

const fetchOutput = async (
  client: SoloClient,
  ref: { process_id?: number; process_name?: string },
  lines: number,
): Promise<string> => {
  const result = await client.callTool<unknown>("get_process_output", {
    ...ref,
    lines,
  });
  if (typeof result === "string") return result;
  if (result && typeof result === "object") {
    const r = result as { output?: string; lines?: string[] };
    if (typeof r.output === "string") return r.output;
    if (Array.isArray(r.lines)) return r.lines.join("\n");
    return JSON.stringify(result, null, 2);
  }
  return "";
};

const sinceToLines = (since: string | undefined, defaultLines: number): number => {
  if (!since) return defaultLines;
  // Best-effort: map any --since value to a larger buffer.
  // Solo's get_process_output is lines-based, not time-based.
  return 500;
};

const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms));

const logsCommand = defineCommand({
  meta: { name: "logs", description: "Show recent output for a process" },
  args: {
    process: { type: "positional", required: true, description: "Process id or name" },
    cwd: { type: "string" },
    follow: { type: "boolean", alias: "f", description: "Follow output (poll)" },
    since: { type: "string", description: "Best-effort time window (maps to a larger line buffer)" },
    lines: { type: "string", description: "Number of lines (default 50)" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const ref = parseProcessRef(String(args.process));
    const baseLines = args.lines ? Number(args.lines) : 50;
    if (!Number.isFinite(baseLines) || baseLines <= 0) {
      writeErr(`--lines must be a positive integer`);
      process.exit(EXIT_USER_ERROR);
    }
    const startLines = sinceToLines(args.since, baseLines);

    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      let prev = await fetchOutput(client, ref, startLines);
      if (args.json) {
        writeJson({ output: prev });
      } else {
        writeOut(prev);
      }
      if (!args.follow) return;

      while (true) {
        await sleep(1000);
        const next = await fetchOutput(client, ref, baseLines);
        if (next === prev) continue;
        // Find longest common suffix of prev and prefix of next
        const overlap = findOverlap(prev, next);
        const delta = next.slice(overlap);
        if (delta.length > 0) writeOut(delta);
        prev = next;
      }
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

const findOverlap = (prev: string, next: string): number => {
  const max = Math.min(prev.length, next.length);
  for (let n = max; n > 0; n--) {
    if (prev.slice(prev.length - n) === next.slice(0, n)) return n;
  }
  return 0;
};

const grepCommand = defineCommand({
  meta: { name: "grep", description: "Search a process's output for a pattern" },
  args: {
    process: { type: "positional", required: true },
    pattern: { type: "positional", required: true },
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const ref = parseProcessRef(String(args.process));
    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const result = await client.callTool<unknown>("search_output", {
        ...ref,
        pattern: String(args.pattern),
      });
      if (args.json) {
        writeJson(result);
      } else if (typeof result === "string") {
        writeOut(result);
      } else {
        writeOut(JSON.stringify(result, null, 2));
      }
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

const statusCommand = defineCommand({
  meta: { name: "status", description: "Show detailed status for a process" },
  args: {
    process: { type: "positional", required: true },
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const ref = parseProcessRef(String(args.process));
    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const status = await client.callTool<Record<string, unknown>>(
        "get_process_status",
        ref,
      );
      printObject(status, { json: args.json, quiet: args.quiet });
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

const simpleAction = (
  name: string,
  description: string,
  toolName: string,
) =>
  defineCommand({
    meta: { name, description },
    args: {
      process: { type: "positional", required: true },
      cwd: { type: "string" },
      json: { type: "boolean" },
      quiet: { type: "boolean", alias: "q" },
    },
    async run({ args }) {
      const ref = parseProcessRef(String(args.process));
      const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
      try {
        const result = await client.callTool<unknown>(toolName, ref);
        if (args.json) {
          writeJson(result ?? { ok: true });
        } else if (!args.quiet) {
          writeOut(`${name}: ${args.process} ✓`);
        }
      } catch (err) {
        handleSoloError(err);
      } finally {
        await dispose();
      }
    },
  });

export const procCommand = defineCommand({
  meta: { name: "proc", description: "Manage Solo processes" },
  subCommands: {
    ls: lsCommand,
    logs: logsCommand,
    grep: grepCommand,
    status: statusCommand,
    stop: simpleAction("stop", "Stop a process", "stop_process"),
    restart: simpleAction("restart", "Restart a process", "restart_process"),
    kill: simpleAction("kill", "Close a process", "close_process"),
  },
});
