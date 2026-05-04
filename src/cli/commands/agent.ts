import { defineCommand } from "citty";
import { connectSolo, handleSoloError, EXIT_USER_ERROR } from "../connect.js";
import { listAgentTiers } from "../../tools/list-agent-tiers.js";
import { resolveAgentTool } from "../../resolver.js";
import { writeErr, writeJson, writeOut, printResult, renderTable } from "../output.js";
import { TierUnavailableError, UnsupportedTierError, TIER_LABELS } from "../../errors.js";

const tierArg = (input: unknown): "small" | "medium" | "large" | null => {
  const tier = String(input ?? "");
  if (tier === "small" || tier === "medium" || tier === "large") return tier;
  return null;
};

const listCommand = defineCommand({
  meta: { name: "list", description: "List agent tools grouped by tier" },
  args: {
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const tiers = await listAgentTiers(client);
      if (args.json) {
        writeJson(tiers);
        return;
      }
      if (args.quiet) return;
      const rows: { tier: string; default: string; alts: string }[] = [];
      for (const tier of TIER_LABELS) {
        const t = tiers[tier];
        rows.push({
          tier,
          default: t.default ? `${t.default.tool_name} (#${t.default.agent_tool_id})` : "—",
          alts: t.alternatives.map((a) => a.tool_name).join(", ") || "—",
        });
      }
      writeOut(
        renderTable(rows, [
          { header: "TIER", get: (r) => r.tier },
          { header: "DEFAULT", get: (r) => r.default, truncate: 50 },
          { header: "ALTERNATIVES", get: (r) => r.alts, truncate: 60 },
        ]),
      );
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

const resolveCommand = defineCommand({
  meta: { name: "resolve", description: "Resolve which agent tool would be selected for a tier" },
  args: {
    tier: { type: "positional", required: true, description: "Tier (small | medium | large)" },
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const tier = tierArg(args.tier);
    if (!tier) {
      writeErr(`Unknown tier "${args.tier}". Expected: ${TIER_LABELS.join(", ")}.`);
      process.exit(EXIT_USER_ERROR);
    }
    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const tools = await client.listAgentTools();
      try {
        const resolution = resolveAgentTool(tools, tier);
        if (args.json) {
          writeJson(resolution);
        } else if (args.quiet) {
          writeOut(String(resolution.selected.agent_tool_id));
        } else {
          writeOut(`tier:        ${tier}`);
          writeOut(`tool_id:     ${resolution.selected.agent_tool_id}`);
          writeOut(`tool_name:   ${resolution.selected.tool_name}`);
          writeOut(`command:     ${resolution.selected.command}`);
          writeOut(`source:      ${resolution.classification_source}`);
        }
      } catch (e) {
        if (e instanceof UnsupportedTierError) {
          writeErr(`Unsupported tier "${e.requested}". Supported: ${TIER_LABELS.join(", ")}.`);
          process.exit(EXIT_USER_ERROR);
        }
        if (e instanceof TierUnavailableError) {
          writeErr(`No enabled candidates available for tier "${e.diagnostics.requested_tier}".`);
          process.exit(EXIT_USER_ERROR);
        }
        throw e;
      }
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

const spawnCommand = defineCommand({
  meta: { name: "spawn", description: "Spawn an agent process for a tier" },
  args: {
    tier: { type: "positional", required: true, description: "Tier (small | medium | large)" },
    name: { type: "string", description: "Process name" },
    "project-id": { type: "string", description: "Override Solo project ID" },
    prompt: { type: "string", description: "Bootstrap prompt delivered as the agent's first message" },
    cwd: { type: "string" },
    json: { type: "boolean" },
    quiet: { type: "boolean", alias: "q" },
  },
  async run({ args }) {
    const tier = tierArg(args.tier);
    if (!tier) {
      writeErr(`Unknown tier "${args.tier}". Expected: ${TIER_LABELS.join(", ")}.`);
      process.exit(EXIT_USER_ERROR);
    }
    let projectId: number | undefined;
    if (args["project-id"]) {
      const n = Number(args["project-id"]);
      if (!Number.isInteger(n) || n < 0) {
        writeErr(`--project-id must be a non-negative integer`);
        process.exit(EXIT_USER_ERROR);
      }
      projectId = n;
    }

    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const tools = await client.listAgentTools();
      const resolution = resolveAgentTool(tools, tier);
      const spawnArgs: {
        kind: "agent";
        agent_tool_id: number;
        name?: string;
        project_id?: number;
        prompt?: string;
      } = {
        kind: "agent",
        agent_tool_id: resolution.selected.agent_tool_id,
      };
      if (args.name) spawnArgs.name = args.name;
      if (projectId !== undefined) spawnArgs.project_id = projectId;
      if (args.prompt) spawnArgs.prompt = args.prompt;

      const spawned = await client.spawnProcess(spawnArgs);
      const result = {
        process_id: spawned.process_id,
        name: spawned.name,
        tier,
        agent_tool_id: resolution.selected.agent_tool_id,
        agent_tool_name: resolution.selected.tool_name,
        project_id: projectId ?? client.projectId,
      };
      if (args.json) {
        writeJson(result);
      } else if (args.quiet) {
        writeOut(String(result.process_id));
      } else {
        writeOut(`process_id:  ${result.process_id}`);
        writeOut(`name:        ${result.name}`);
        writeOut(`tier:        ${result.tier}`);
        writeOut(`tool:        ${result.agent_tool_name} (#${result.agent_tool_id})`);
        if (result.project_id !== undefined)
          writeOut(`project_id:  ${result.project_id}`);
      }
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});

export const agentCommand = defineCommand({
  meta: { name: "agent", description: "Manage Duo agent tiers" },
  subCommands: {
    list: listCommand,
    resolve: resolveCommand,
    spawn: spawnCommand,
  },
});
