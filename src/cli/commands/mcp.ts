import { defineCommand } from "citty";
import { runServer } from "../../server.js";
import { writeErr } from "../output.js";
import { EXIT_USER_ERROR } from "../connect.js";

export const mcpCommand = defineCommand({
  meta: {
    name: "mcp",
    description: "Start the Duo MCP server (stdio transport)",
  },
  args: {
    cwd: {
      type: "string",
      description: "Working directory (defaults to process.cwd())",
    },
  },
  async run({ args }) {
    try {
      await runServer({ cwd: args.cwd });
    } catch (err) {
      writeErr(err instanceof Error ? err.message : String(err));
      process.exit(EXIT_USER_ERROR);
    }
  },
});
