#!/usr/bin/env node
// Empirical verification: does Solo's bind_session_process persist across
// subsequent tools/call requests on the same MCP session?
//
// Plan:
//   1. Open one Solo MCP session via SoloClient (single stdio transport).
//   2. list_projects → pick the duo project (or first available).
//   3. spawn_process to obtain a fresh process_id.
//   4. bind_session_process with that process_id.
//   5. Call whoami WITHOUT explicit process_id; expect the bound process_id back.
//   6. As a control: bind_session_process expectation — also call a process-scoped
//      tool like scratchpad_list with no explicit process_id.
//
// Run:  node notes/manual-testing/scripts/verify-bind-session.mjs

import { StdioTransport } from "../../../dist/transport/stdio.js";
import { SoloClient } from "../../../dist/solo-client.js";

const transport = new StdioTransport({
  type: "stdio",
  command: "/Applications/Solo.app/Contents/MacOS/mcp",
  args: [],
});

// We need raw access to tools/call for tools the SoloClient doesn't wrap.
// Reach in via the private _request — this is a throwaway script.
const client = new SoloClient(transport);
await client.connect();

const rawCall = (name, args = {}) =>
  client["_request"]("tools/call", { name, arguments: args });

const text = (result) => {
  const c = result?.content?.find?.((x) => x.type === "text");
  return c?.text;
};

const logStep = (label, value) => {
  console.log(`\n=== ${label} ===`);
  console.log(typeof value === "string" ? value : JSON.stringify(value, null, 2));
};

try {
  // 1. list_projects
  const projectsRaw = await rawCall("list_projects");
  const projects = JSON.parse(text(projectsRaw));
  logStep("list_projects", projects);

  const cwd = process.cwd();
  const match = projects
    .filter((p) => cwd.startsWith(p.path))
    .sort((a, b) => b.path.length - a.path.length)[0];
  if (!match) throw new Error(`no project matched cwd ${cwd}`);
  logStep("matched project", match);

  // 2. list_agent_tools to pick something to spawn
  const toolsRaw = await rawCall("list_agent_tools", { project_id: match.id });
  const tools = JSON.parse(text(toolsRaw));
  const enabled = tools.find((t) => t.enabled);
  if (!enabled) throw new Error("no enabled agent tool");
  logStep("agent tool to spawn", enabled);

  // 3. spawn a process
  const spawnedRaw = await rawCall("spawn_process", {
    kind: "agent",
    agent_tool_id: enabled.id,
    project_id: match.id,
    name: `bind-test-${Date.now()}`,
  });
  const spawned = JSON.parse(text(spawnedRaw));
  logStep("spawn_process result", spawned);
  const processId = spawned.process_id;

  // 4. bind_session_process
  const bindRaw = await rawCall("bind_session_process", { process_id: processId });
  logStep("bind_session_process result", bindRaw);

  // 5. whoami WITHOUT explicit process_id — does it return the bound id?
  const whoamiRaw = await rawCall("whoami");
  logStep("whoami after bind (no args)", whoamiRaw);
  const whoami = JSON.parse(text(whoamiRaw) ?? "{}");

  // 6. additional control: scratchpad_list without process_id (process-scoped)
  let scratchpadResult;
  try {
    const spRaw = await rawCall("scratchpad_list");
    scratchpadResult = { ok: true, value: spRaw };
  } catch (e) {
    scratchpadResult = { ok: false, error: String(e) };
  }
  logStep("scratchpad_list after bind (no args)", scratchpadResult);

  // Verdict
  const boundId = whoami.process_id ?? whoami.processId ?? whoami.bound_process_id;
  const matchesBound = boundId && String(boundId) === String(processId);
  logStep("VERDICT", {
    spawned_process_id: processId,
    whoami_process_id: boundId,
    matches: matchesBound,
    scratchpad_no_arg_ok: scratchpadResult.ok,
  });

  // 7. clean up: close the spawned process
  try {
    await rawCall("close_process", { process_id: processId });
  } catch (e) {
    console.error("close_process failed:", e);
  }

  await client.disconnect();
  process.exit(matchesBound ? 0 : 1);
} catch (err) {
  console.error("FAIL:", err);
  try {
    await client.disconnect();
  } catch {}
  process.exit(2);
}
