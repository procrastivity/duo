import { defineCommand } from "citty";
import { connectSolo, handleSoloError } from "../connect.js";
import { printObject } from "../output.js";

export const whoamiCommand = defineCommand({
  meta: {
    name: "whoami",
    description: "Show the resolved Solo project and bound process for this session",
  },
  args: {
    cwd: { type: "string", description: "Working directory" },
    json: { type: "boolean", description: "Emit JSON" },
    quiet: { type: "boolean", alias: "q", description: "Suppress connect logs" },
  },
  async run({ args }) {
    const { client, dispose } = await connectSolo({ cwd: args.cwd, quiet: args.quiet });
    try {
      const projectId = client.projectId;
      const processId = client.processId;
      let projectName: string | undefined;
      let projectPath: string | undefined;
      if (projectId !== undefined) {
        try {
          const projects = await client.listProjects();
          const match = projects.find((p) => p.id === projectId);
          projectName = match?.name;
          projectPath = match?.path;
        } catch {
          // best-effort
        }
      }
      printObject(
        {
          project_id: projectId,
          project_name: projectName,
          project_path: projectPath,
          process_id: processId,
        },
        { json: args.json, quiet: args.quiet },
      );
    } catch (err) {
      handleSoloError(err);
    } finally {
      await dispose();
    }
  },
});
