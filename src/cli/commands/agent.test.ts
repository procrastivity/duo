import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runCommand } from "citty";
import type { Presets } from "../../types/presets.js";

// --- Mocks -----------------------------------------------------------------
// `duo agent list|resolve` read config via loadConfig; `spawn` connects to Solo.
// Both are mocked so the tests stay filesystem- and Solo-free. `resolvePreset`
// itself is the REAL implementation; the fixtures use provider-less defs so the
// (real) filesystem-backed isProviderEnabled reads them as enabled.

const h = vi.hoisted(() => ({
  spawnProcess: vi.fn(),
  loadConfig: vi.fn(),
  connectSolo: vi.fn(),
  dispose: vi.fn(async () => {}),
}));

vi.mock("../config-loader.js", () => ({
  loadConfig: h.loadConfig,
}));

vi.mock("../connect.js", () => ({
  connectSolo: h.connectSolo,
  handleSoloError: (err: unknown) => {
    throw err;
  },
  EXIT_USER_ERROR: 1,
}));

import { agentCommand } from "./agent.js";

const presets: Presets = {
  builder: [{ id: "b", agent_tool_id: 2 }],
  withArgs: [{ id: "wa", agent_tool_id: 3, extra_args: "--model sonnet --json" }],
};

let stdout: string[];

beforeEach(() => {
  vi.clearAllMocks();
  stdout = [];
  vi.spyOn(process.stdout, "write").mockImplementation((chunk: unknown) => {
    stdout.push(String(chunk));
    return true;
  });
  h.loadConfig.mockReturnValue({
    config: { presets },
    configPath: "/x",
    policyPath: null,
    usedDefaults: false,
  });
  h.spawnProcess.mockResolvedValue({ process_id: 555, name: "agent-x" });
  h.connectSolo.mockResolvedValue({
    client: { spawnProcess: h.spawnProcess, projectId: 6 },
    config: { config: { presets } },
    dispose: h.dispose,
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

const out = () => stdout.join("");

describe("duo agent list", () => {
  it("emits the preset view as JSON", async () => {
    await runCommand(agentCommand, { rawArgs: ["list", "--json"] });
    const parsed = JSON.parse(out());
    expect(Object.keys(parsed).sort()).toEqual(["builder", "withArgs"]);
    expect(parsed.builder.available).toBe(true);
  });
});

describe("duo agent resolve", () => {
  it("resolves a preset and prints the tool id (json)", async () => {
    await runCommand(agentCommand, { rawArgs: ["resolve", "builder", "--json"] });
    const parsed = JSON.parse(out());
    expect(parsed.agent_tool_id).toBe(2);
    expect(parsed.preset_used).toBe("builder");
  });

  it("surfaces tokenized extra_args in the resolution", async () => {
    await runCommand(agentCommand, { rawArgs: ["resolve", "withArgs", "--json"] });
    const parsed = JSON.parse(out());
    expect(parsed.extra_args).toEqual(["--model", "sonnet", "--json"]);
  });

  it("--quiet prints just the tool id", async () => {
    await runCommand(agentCommand, { rawArgs: ["resolve", "builder", "--quiet"] });
    expect(out().trim()).toBe("2");
  });
});

describe("duo agent spawn", () => {
  it("threads the resolved, tokenized extra_args into spawnProcess", async () => {
    await runCommand(agentCommand, { rawArgs: ["spawn", "withArgs", "--json"] });
    expect(h.spawnProcess).toHaveBeenCalledTimes(1);
    const args = h.spawnProcess.mock.calls[0][0];
    expect(args.agent_tool_id).toBe(3);
    expect(args.extra_args).toEqual(["--model", "sonnet", "--json"]);
  });

  it("omits extra_args when the preset has none", async () => {
    await runCommand(agentCommand, { rawArgs: ["spawn", "builder", "--json"] });
    const args = h.spawnProcess.mock.calls[0][0];
    expect(args.agent_tool_id).toBe(2);
    expect(args).not.toHaveProperty("extra_args");
  });

  it("returns the preset-shaped result and disposes the connection", async () => {
    await runCommand(agentCommand, { rawArgs: ["spawn", "builder", "--json"] });
    const parsed = JSON.parse(out());
    expect(parsed.process_id).toBe(555);
    expect(parsed.preset).toBe("builder");
    expect(parsed.agent_tool_id).toBe(2);
    expect(h.dispose).toHaveBeenCalled();
  });
});
