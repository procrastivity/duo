import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { parse as parseYaml } from "yaml";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { SoloAgentTool } from "../../types/solo.js";
import {
  presetAdd,
  resolvePresetAgentTool,
  readPresets,
  buildPresetView,
  tryLoadToolNames,
  type AgentToolLister,
} from "./preset.js";
import type { Presets } from "../../types/presets.js";

const tool = (
  id: number,
  name: string,
  enabled = true,
  command = "claude",
): SoloAgentTool => ({ id, name, command, tool_type: "generic", enabled });

// Mirrors the live install described in the workplan (D4).
const TOOLS: SoloAgentTool[] = [
  tool(3, "Claude"),
  tool(4, "Codex"),
  tool(17, "Codex • GPT 5.5"),
];

describe("resolvePresetAgentTool (D4)", () => {
  it("all-digits selector resolves an existing id", () => {
    expect(resolvePresetAgentTool(TOOLS, "4")).toEqual({
      ok: true,
      agent_tool_id: 4,
      tool: TOOLS[1],
    });
  });

  it("all-digits selector for a missing id → notFound", () => {
    expect(resolvePresetAgentTool(TOOLS, "999")).toEqual({ notFound: true });
  });

  it("all-digits selector for a disabled tool still resolves ok (persist + warn is the caller's job)", () => {
    const tools = [tool(4, "Codex", false)];
    const res = resolvePresetAgentTool(tools, "4");
    expect(res).toMatchObject({ ok: true, agent_tool_id: 4 });
    if ("ok" in res) expect(res.tool.enabled).toBe(false);
  });

  it("name match is case-insensitive", () => {
    expect(resolvePresetAgentTool(TOOLS, "codex")).toMatchObject({
      ok: true,
      agent_tool_id: 4,
    });
    expect(resolvePresetAgentTool(TOOLS, "CLAUDE")).toMatchObject({
      ok: true,
      agent_tool_id: 3,
    });
  });

  it("name match is EXACT, not substring (`codex` ≠ `Codex • GPT 5.5`)", () => {
    const res = resolvePresetAgentTool(TOOLS, "codex");
    expect(res).toMatchObject({ ok: true, agent_tool_id: 4 });
  });

  it("ambiguous name (two same-named tools) → ambiguous with candidates", () => {
    const tools = [tool(4, "Codex"), tool(8, "codex"), tool(3, "Claude")];
    const res = resolvePresetAgentTool(tools, "codex");
    expect(res).toMatchObject({ ambiguous: true });
    if ("ambiguous" in res) {
      expect(res.candidates.map((t) => t.id).sort()).toEqual([4, 8]);
    }
  });

  it("no name match → notFound", () => {
    expect(resolvePresetAgentTool(TOOLS, "opencode")).toEqual({ notFound: true });
  });
});

const fakeClient = (tools: SoloAgentTool[]): AgentToolLister => ({
  listAgentTools: async () => tools,
});

describe("presetAdd", () => {
  const originalEnv = process.env;
  let tmp: string;
  let configPath: string;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.DUO_CONFIG;
    delete process.env.XDG_CONFIG_HOME;
    delete process.env.DUO_POLICY;
    tmp = mkdtempSync(join(tmpdir(), "duo-preset-add-"));
    configPath = join(tmp, "config.yaml");
    process.env.DUO_CONFIG = configPath;
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  it("writes the expected definition on success (id/agent_tool_id/extra_args/provider)", async () => {
    const result = await presetAdd(fakeClient(TOOLS), {
      name: "builder",
      agentTool: "Codex",
      extraArgs: "-m sonnet",
      provider: "anthropic",
    });

    expect(result.status).toBe("written");
    if (result.status !== "written") throw new Error("expected written");
    expect(result.definition.agent_tool_id).toBe(4);
    expect(result.definition.extra_args).toBe("-m sonnet");
    expect(result.definition.provider).toBe("anthropic");
    expect(result.definition.id).toMatch(/^[0-9a-z]{8}$/);
    expect(result.disabledWarning).toBe(false);

    const onDisk = parseYaml(readFileSync(configPath, "utf8"));
    expect(onDisk.presets.builder).toEqual([
      {
        id: result.definition.id,
        agent_tool_id: 4,
        extra_args: "-m sonnet",
        provider: "anthropic",
      },
    ]);
  });

  it("resolves an all-digits selector identically", async () => {
    const result = await presetAdd(fakeClient(TOOLS), { name: "builder", agentTool: "4" });
    expect(result.status).toBe("written");
    if (result.status !== "written") throw new Error("expected written");
    expect(result.definition.agent_tool_id).toBe(4);
    expect(result.definition.extra_args).toBeUndefined();
    expect(result.definition.provider).toBeUndefined();
  });

  it("always appends (D5): a second add to the same preset keeps both, with distinct ids", async () => {
    const first = await presetAdd(fakeClient(TOOLS), { name: "builder", agentTool: "4" });
    const second = await presetAdd(fakeClient(TOOLS), { name: "builder", agentTool: "4" });
    if (first.status !== "written" || second.status !== "written") throw new Error("expected written");
    expect(first.definition.id).not.toBe(second.definition.id);

    const onDisk = parseYaml(readFileSync(configPath, "utf8"));
    expect(onDisk.presets.builder).toHaveLength(2);
  });

  it("warns but persists when the matched tool is disabled", async () => {
    const tools = [tool(4, "Codex", false)];
    const result = await presetAdd(fakeClient(tools), { name: "builder", agentTool: "4" });
    expect(result.status).toBe("written");
    if (result.status !== "written") throw new Error("expected written");
    expect(result.disabledWarning).toBe(true);
    expect(existsSync(configPath)).toBe(true);
  });

  it("ambiguous selector writes NOTHING and reports candidates", async () => {
    const tools = [tool(4, "Codex"), tool(8, "codex")];
    const result = await presetAdd(fakeClient(tools), { name: "builder", agentTool: "codex" });
    expect(result.status).toBe("ambiguous");
    if (result.status !== "ambiguous") throw new Error("expected ambiguous");
    expect(result.candidates.map((t) => t.id).sort()).toEqual([4, 8]);
    expect(existsSync(configPath)).toBe(false);
  });

  it("unknown selector writes NOTHING and reports the full tool list", async () => {
    const result = await presetAdd(fakeClient(TOOLS), { name: "builder", agentTool: "nope" });
    expect(result.status).toBe("not_found");
    if (result.status !== "not_found") throw new Error("expected not_found");
    expect(result.tools).toHaveLength(3);
    expect(existsSync(configPath)).toBe(false);
  });

  it("does not persist a policy key into config.yaml even with duo.policy.yaml present (D3)", async () => {
    const policyPath = join(tmp, "duo.policy.yaml");
    process.env.DUO_POLICY = policyPath;
    writeFileSync(policyPath, "command_tokens:\n  small:\n    tokens:\n      - foo\n");

    const result = await presetAdd(fakeClient(TOOLS), { name: "builder", agentTool: "4" });
    expect(result.status).toBe("written");
    const text = readFileSync(configPath, "utf8");
    expect(text).not.toContain("policy");
  });
});

// A fixture config with two presets, one carrying multiple definitions.
const FIXTURE_CONFIG = [
  "solo:",
  "  transport:",
  "    type: stdio",
  "presets:",
  "  builder:",
  "    - id: aaaa1111",
  "      agent_tool_id: 4",
  "      extra_args: -m sonnet",
  "      provider: anthropic",
  "    - id: bbbb2222",
  "      agent_tool_id: 3",
  "  reviewer:",
  "    - id: cccc3333",
  "      agent_tool_id: 17",
  "",
].join("\n");

describe("readPresets (offline)", () => {
  const originalEnv = process.env;
  let tmp: string;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.DUO_CONFIG;
    delete process.env.XDG_CONFIG_HOME;
    delete process.env.DUO_POLICY;
    tmp = mkdtempSync(join(tmpdir(), "duo-preset-list-"));
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  it("returns the parsed presets section from a fixture config", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, FIXTURE_CONFIG);
    process.env.DUO_CONFIG = path;
    expect(readPresets()).toEqual({
      builder: [
        { id: "aaaa1111", agent_tool_id: 4, extra_args: "-m sonnet", provider: "anthropic" },
        { id: "bbbb2222", agent_tool_id: 3 },
      ],
      reviewer: [{ id: "cccc3333", agent_tool_id: 17 }],
    });
  });

  it("returns {} when there is no presets section (empty case)", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "solo:\n  transport:\n    type: stdio\n");
    process.env.DUO_CONFIG = path;
    expect(readPresets()).toEqual({});
  });

  it("returns {} when the config file is absent", () => {
    process.env.DUO_CONFIG = join(tmp, "missing.yaml");
    expect(readPresets()).toEqual({});
  });

  it("throws a readable error when the presets section is malformed", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(
      path,
      "solo:\n  transport:\n    type: stdio\npresets:\n  builder:\n    - agent_tool_id: 4\n",
    );
    process.env.DUO_CONFIG = path;
    // Missing the required `id` — the Task-1 schema rejects it.
    expect(() => readPresets()).toThrow();
  });
});

const PRESETS: Presets = {
  builder: [
    { id: "aaaa1111", agent_tool_id: 4, extra_args: "-m sonnet", provider: "anthropic" },
    { id: "bbbb2222", agent_tool_id: 3 },
  ],
  reviewer: [{ id: "cccc3333", agent_tool_id: 17 }],
};

describe("buildPresetView", () => {
  it("flattens every preset → definition into rows (id-only when no live names)", () => {
    const view = buildPresetView(PRESETS);
    expect(view.filterMissing).toBe(false);
    expect(view.rows).toEqual([
      { preset: "builder", id: "aaaa1111", agent_tool_id: 4, tool: "#4", extra_args: "-m sonnet", provider: "anthropic" },
      { preset: "builder", id: "bbbb2222", agent_tool_id: 3, tool: "#3", extra_args: "—", provider: "—" },
      { preset: "reviewer", id: "cccc3333", agent_tool_id: 17, tool: "#17", extra_args: "—", provider: "—" },
    ]);
  });

  it("enriches the tool column with live names when a map is supplied", () => {
    const names = new Map<number, string>([
      [4, "Codex"],
      [3, "Claude"],
      // 17 intentionally absent → renders (unknown tool) (OQ3)
    ]);
    const view = buildPresetView(PRESETS, { toolNames: names });
    expect(view.rows.map((r) => r.tool)).toEqual([
      "Codex (#4)",
      "Claude (#3)",
      "(unknown tool) (#17)",
    ]);
  });

  it("filters to a single preset by name", () => {
    const view = buildPresetView(PRESETS, { filter: "reviewer" });
    expect(view.filterMissing).toBe(false);
    expect(view.presets).toEqual({ reviewer: [{ id: "cccc3333", agent_tool_id: 17 }] });
    expect(view.rows).toHaveLength(1);
    expect(view.rows[0]!.preset).toBe("reviewer");
  });

  it("flags a filter that matches no preset (empty view, filterMissing=true)", () => {
    const view = buildPresetView(PRESETS, { filter: "ghost" });
    expect(view.filterMissing).toBe(true);
    expect(view.presets).toEqual({});
    expect(view.rows).toEqual([]);
  });

  it("empty/no-presets case yields no rows", () => {
    const view = buildPresetView({});
    expect(view.filterMissing).toBe(false);
    expect(view.rows).toEqual([]);
  });
});

describe("tryLoadToolNames (graceful degrade — never throws)", () => {
  const originalEnv = process.env;
  let tmp: string;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.DUO_CONFIG;
    delete process.env.XDG_CONFIG_HOME;
    delete process.env.DUO_POLICY;
    tmp = mkdtempSync(join(tmpdir(), "duo-preset-enrich-"));
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  it("returns null (does not throw) when Solo is unreachable", async () => {
    // A config whose transport command does not exist → connect fails → null.
    const path = join(tmp, "config.yaml");
    writeFileSync(
      path,
      "solo:\n  transport:\n    type: stdio\n    command: this-command-does-not-exist-duo\n",
    );
    process.env.DUO_CONFIG = path;
    await expect(tryLoadToolNames({ cwd: tmp })).resolves.toBeNull();
  });

  it("returns null when config loading itself fails", async () => {
    // A non-mapping config makes loadConfig throw; the helper still degrades.
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "- not\n- a\n- mapping\n");
    process.env.DUO_CONFIG = path;
    await expect(tryLoadToolNames({ cwd: tmp })).resolves.toBeNull();
  });
});
