import { defineCommand, runMain } from "citty";
import { mcpCommand } from "./commands/mcp.js";
import { versionCommand } from "./commands/version.js";
import { configCommand } from "./commands/config.js";
import { whoamiCommand } from "./commands/whoami.js";
import { projectCommand } from "./commands/project.js";
import { agentCommand } from "./commands/agent.js";
import { procCommand } from "./commands/proc.js";
import { doctorCommand } from "./commands/doctor.js";

export const main = defineCommand({
  meta: {
    name: "duo",
    description: "Duo: Solo MCP companion + control-plane CLI",
  },
  subCommands: {
    mcp: mcpCommand,
    agent: agentCommand,
    proc: procCommand,
    project: projectCommand,
    whoami: whoamiCommand,
    doctor: doctorCommand,
    version: versionCommand,
    config: configCommand,
  },
});

export const run = (): Promise<void> => runMain(main);
