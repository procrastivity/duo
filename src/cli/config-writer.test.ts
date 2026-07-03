import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { parse as parseYaml } from "yaml";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadConfig } from "./config-loader.js";
import { generateDefinitionId, readRawConfig, writeConfig } from "./config-writer.js";

describe("readRawConfig", () => {
  const originalEnv = process.env;
  let tmp: string;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.DUO_CONFIG;
    delete process.env.XDG_CONFIG_HOME;
    tmp = mkdtempSync(join(tmpdir(), "duo-config-writer-"));
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  it("seeds from DEFAULT_RAW_CONFIG when the file is absent", () => {
    process.env.DUO_CONFIG = join(tmp, "missing.yaml");
    const raw = readRawConfig();
    expect(raw).toEqual({ solo: { transport: { type: "stdio" } } });
  });

  it("seeds from DEFAULT_RAW_CONFIG when the file is empty / comments-only", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "# just a comment\n   \n");
    process.env.DUO_CONFIG = path;
    expect(readRawConfig()).toEqual({ solo: { transport: { type: "stdio" } } });
  });

  it("returns the parsed mapping when the file is populated", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "solo:\n  transport:\n    type: stdio\npresets:\n  builder:\n    - id: abc12345\n      agent_tool_id: 4\n");
    process.env.DUO_CONFIG = path;
    const raw = readRawConfig();
    expect(raw.presets).toEqual({ builder: [{ id: "abc12345", agent_tool_id: 4 }] });
  });

  it("returns a fresh object (mutations do not leak into the default seed)", () => {
    process.env.DUO_CONFIG = join(tmp, "missing.yaml");
    const a = readRawConfig() as Record<string, unknown>;
    a.presets = { builder: [] };
    const b = readRawConfig() as Record<string, unknown>;
    expect(b.presets).toBeUndefined();
  });

  it("throws when the file parses to a non-mapping", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "- just\n- a\n- list\n");
    process.env.DUO_CONFIG = path;
    expect(() => readRawConfig()).toThrow(/must be a mapping/);
  });
});

describe("writeConfig", () => {
  const originalEnv = process.env;
  let tmp: string;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.DUO_CONFIG;
    delete process.env.XDG_CONFIG_HOME;
    tmp = mkdtempSync(join(tmpdir(), "duo-config-writer-"));
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  it("round-trips a presets write (write then re-read yields the same object)", () => {
    process.env.DUO_CONFIG = join(tmp, "config.yaml");
    const raw = readRawConfig();
    raw.presets = {
      builder: [{ id: "def45678", agent_tool_id: 4, extra_args: "-m sonnet", provider: "anthropic" }],
    };
    writeConfig(raw);

    const reread = readRawConfig();
    expect(reread).toEqual(raw);
    expect(reread.presets).toEqual({
      builder: [{ id: "def45678", agent_tool_id: 4, extra_args: "-m sonnet", provider: "anthropic" }],
    });
  });

  it("creates missing parent directories", () => {
    const path = join(tmp, "nested", "deep", "config.yaml");
    process.env.DUO_CONFIG = path;
    writeConfig({ solo: { transport: { type: "stdio" } } });
    expect(existsSync(path)).toBe(true);
  });

  it("does not inject schema defaults (e.g. transport.args) into the on-disk file", () => {
    const path = join(tmp, "config.yaml");
    process.env.DUO_CONFIG = path;
    writeConfig({ solo: { transport: { type: "stdio" } } });
    const onDisk = parseYaml(readFileSync(path, "utf8"));
    expect(onDisk).toEqual({ solo: { transport: { type: "stdio" } } });
    expect(onDisk.solo.transport.args).toBeUndefined();
  });

  it("rejects producing a config that fails soloConfigSchema, and persists nothing", () => {
    const path = join(tmp, "config.yaml");
    process.env.DUO_CONFIG = path;
    // Seed a valid config on disk first.
    writeConfig({ solo: { transport: { type: "stdio" } } });
    const before = readFileSync(path, "utf8");

    // A preset definition missing the required `id` fails validation.
    expect(() =>
      writeConfig({
        solo: { transport: { type: "stdio" } },
        presets: { builder: [{ agent_tool_id: 4 }] },
      }),
    ).toThrow();

    // The prior valid file is untouched.
    expect(readFileSync(path, "utf8")).toBe(before);
  });

  it("rejects an unknown top-level key (strict schema) without persisting", () => {
    const path = join(tmp, "missing.yaml");
    process.env.DUO_CONFIG = path;
    expect(() => writeConfig({ solo: { transport: { type: "stdio" } }, bogus: true })).toThrow();
    expect(existsSync(path)).toBe(false);
  });

  it("REGRESSION GUARD: a preset write serializes only the raw config.yaml keys (no injected schema defaults)", () => {
    const configPath = join(tmp, "config.yaml");
    process.env.DUO_CONFIG = configPath;

    // Writer starts from the RAW config.yaml (absent → default seed), and only
    // the keys present in that raw view reach disk.
    const raw = readRawConfig();
    raw.presets = { reviewer: [{ id: generateDefinitionId(), agent_tool_id: 4 }] };
    writeConfig(raw);

    const text = readFileSync(configPath, "utf8");
    const reparsed = parseYaml(text) as Record<string, unknown>;
    expect(reparsed.presets).toBeDefined();
    // No schema default (e.g. transport.args) leaked into the on-disk file.
    expect((reparsed.solo as { transport: { args?: unknown } }).transport.args).toBeUndefined();

    // End-to-end: the written presets load back via loadConfig.
    const loaded = loadConfig({ cwd: tmp });
    expect(loaded.config.presets?.reviewer?.[0]?.agent_tool_id).toBe(4);
  });
});

describe("generateDefinitionId", () => {
  it("produces an 8-char base36 id", () => {
    const id = generateDefinitionId();
    expect(id).toMatch(/^[0-9a-z]{8}$/);
  });

  it("never returns an id already in the seeded set", () => {
    const seeded = new Set(Array.from({ length: 500 }, () => generateDefinitionId()));
    const next = generateDefinitionId(seeded);
    expect(seeded.has(next)).toBe(false);
    expect(next).toMatch(/^[0-9a-z]{8}$/);
  });

  it("generates unique ids across a batch when prior ids are fed back in", () => {
    const seen = new Set<string>();
    for (let i = 0; i < 1000; i++) {
      const id = generateDefinitionId(seen);
      expect(seen.has(id)).toBe(false);
      seen.add(id);
    }
    expect(seen.size).toBe(1000);
  });
});
