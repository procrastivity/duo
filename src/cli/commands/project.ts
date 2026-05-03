import { defineCommand } from "citty";
import { connectSolo, handleSoloError } from "../connect.js";
import { printObject, printResult } from "../output.js";

const lsCommand = defineCommand({
  meta: { name: "ls", description: "List Solo projects" },
  args: {
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const projects = await client.listProjects();
      printResult(
        projects,
        [
          { header: "ID", get: (p) => String(p.id) },
          { header: "NAME", get: (p) => p.name, truncate: 32 },
          { header: "PATH", get: (p) => p.path, truncate: 80 },
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

const statusCommand = defineCommand({
  meta: { name: "status", description: "Show status for the current Solo project" },
  args: {
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const status = await client.callTool<Record<string, unknown>>(
        "get_project_status",
        client.projectId !== undefined ? { project_id: client.projectId } : {},
      );
      if (args.json) {
        process.stdout.write(JSON.stringify(status, null, 2) + "\n");
      } else if (!args.quiet) {
        printObject(status, { json: false });
      }
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

export const projectCommand = defineCommand({
  meta: { name: "project", description: "Inspect Solo projects" },
  subCommands: {
    ls: lsCommand,
    status: statusCommand,
  },
});
