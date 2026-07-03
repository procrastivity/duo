import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runCommand } from "citty";
import type { Presets } from "../../types/presets.js";

// --- Mocks -----------------------------------------------------------------
// `duo agent list|resolve` read config via loadConfig; `launch` connects to
// Solo. Both are mocked so the tests stay filesystem- and Solo-free.
// `resolvePreset` is the REAL implementation wrapped in a spy so tests can
// assert the options threaded into it (e.g. `--avoid-provider`); the fixtures
// use provider-less defs so the (real) filesystem-backed isProviderEnabled
// reads them as enabled regardless.

const h = vi.hoisted(() => ({
  spawnProcess: vi.fn(),
  loadConfig: vi.fn(),
  connectSolo: vi.fn(),
  dispose: vi.fn(async () => {}),
  realResolvePreset: undefined as
    | typeof import("../../resolver.js")["resolvePreset"]
    | undefined,
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

vi.mock("../../resolver.js", async (importActual) => {
  const actual = await importActual<typeof import("../../resolver.js")>();
  h.realResolvePreset = actual.resolvePreset;
  return { ...actual, resolvePreset: vi.fn(actual.resolvePreset) };
});

import { agentCommand } from "./agent.js";
import { resolvePreset } from "../../resolver.js";

const resolvePresetMock = vi.mocked(resolvePreset);

const presets: Presets = {
  builder: [{ id: "b", agent_tool_id: 2 }],
  withArgs: [{ id: "wa", agent_tool_id: 3, extra_args: "--model sonnet --json" }],
};

let stdout: string[];

beforeEach(() => {
  vi.clearAllMocks();
  resolvePresetMock.mockImplementation(h.realResolvePreset!);
  stdout = [];
  vi.spyOn(process.stdout, "write").mockImplementation((chunk: unknown) => {
    stdout.push(String(chunk));
    return true;
  });
  h.loadConfig.mockReturnValue({
    config: { presets },
    configPath: "/x",
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

describe("duo agent subcommands", () => {
  it("exposes launch (not spawn)", () => {
    const subs = agentCommand.subCommands as Record<string, unknown>;
    expect(Object.keys(subs).sort()).toEqual(["launch", "list", "resolve"]);
    expect(subs).not.toHaveProperty("spawn");
  });
});

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

  it("threads --avoid-provider into the resolve engine", async () => {
    await runCommand(agentCommand, {
      rawArgs: ["resolve", "builder", "--avoid-provider", "anthropic", "--json"],
    });
    expect(resolvePresetMock).toHaveBeenCalledWith(presets, "builder", {
      avoidProvider: "anthropic",
    });
  });
});

describe("duo agent launch", () => {
  it("threads the resolved, tokenized extra_args into spawnProcess", async () => {
    await runCommand(agentCommand, { rawArgs: ["launch", "withArgs", "--json"] });
    expect(h.spawnProcess).toHaveBeenCalledTimes(1);
    const args = h.spawnProcess.mock.calls[0][0];
    expect(args.agent_tool_id).toBe(3);
    expect(args.extra_args).toEqual(["--model", "sonnet", "--json"]);
  });

  it("appends caller --extra-arguments after preset args", async () => {
    await runCommand(agentCommand, {
      rawArgs: [
        "launch",
        "withArgs",
        "--extra-arguments",
        "--verbose '--label=hello world'",
        "--json",
      ],
    });

    const expected = [
      "--model",
      "sonnet",
      "--json",
      "--verbose",
      "--label=hello world",
    ];
    expect(h.spawnProcess.mock.calls[0][0].extra_args).toEqual(expected);
    expect(JSON.parse(out()).extra_args).toEqual(expected);
  });

  it("sends caller-only --extra-arguments when the preset has none", async () => {
    await runCommand(agentCommand, {
      rawArgs: ["launch", "builder", "--extra-arguments", "--only-caller", "--json"],
    });

    expect(h.spawnProcess.mock.calls[0][0].extra_args).toEqual(["--only-caller"]);
    expect(JSON.parse(out()).extra_args).toEqual(["--only-caller"]);
  });

  it("omits extra_args when the preset has none", async () => {
    await runCommand(agentCommand, { rawArgs: ["launch", "builder", "--json"] });
    const args = h.spawnProcess.mock.calls[0][0];
    expect(args.agent_tool_id).toBe(2);
    expect(args).not.toHaveProperty("extra_args");
  });

  it("returns the preset-shaped result and disposes the connection", async () => {
    await runCommand(agentCommand, { rawArgs: ["launch", "builder", "--json"] });
    const parsed = JSON.parse(out());
    expect(parsed.process_id).toBe(555);
    expect(parsed.preset).toBe("builder");
    expect(parsed.agent_tool_id).toBe(2);
    expect(h.dispose).toHaveBeenCalled();
  });

  it("threads --avoid-provider into the launch engine", async () => {
    await runCommand(agentCommand, {
      rawArgs: ["launch", "builder", "--avoid-provider", "anthropic", "--json"],
    });
    expect(resolvePresetMock).toHaveBeenCalledWith(presets, "builder", {
      avoidProvider: "anthropic",
    });
  });
});
