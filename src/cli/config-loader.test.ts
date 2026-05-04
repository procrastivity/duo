import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadConfig, resolveConfigPath } from "./config-loader.js";

describe("resolveConfigPath", () => {
  const originalEnv = process.env;

  beforeEach(() => {
    // Reset env vars before each test
    process.env = { ...originalEnv };
    delete process.env.DUO_CONFIG;
    delete process.env.XDG_CONFIG_HOME;
  });

  afterEach(() => {
    process.env = originalEnv;
  });

  it("returns DUO_CONFIG verbatim when set", () => {
    process.env.DUO_CONFIG = "/explicit/path/to/config.yaml";
    expect(resolveConfigPath()).toBe("/explicit/path/to/config.yaml");
  });

  it("DUO_CONFIG takes priority over XDG_CONFIG_HOME", () => {
    process.env.DUO_CONFIG = "/explicit/path/to/config.yaml";
    process.env.XDG_CONFIG_HOME = "/custom/xdg";
    expect(resolveConfigPath()).toBe("/explicit/path/to/config.yaml");
  });

  it("returns XDG_CONFIG_HOME/duo/config.yaml when XDG_CONFIG_HOME is set", () => {
    process.env.XDG_CONFIG_HOME = "/custom/xdg";
    expect(resolveConfigPath()).toBe("/custom/xdg/duo/config.yaml");
  });

  it("falls back to ~/.config/duo/config.yaml when neither DUO_CONFIG nor XDG_CONFIG_HOME is set", () => {
    const expected = join(homedir(), ".config", "duo", "config.yaml");
    expect(resolveConfigPath()).toBe(expected);
  });

  it("no longer accepts a cwd argument (function signature is zero-arg)", () => {
    // Ensures the old cwd-relative API is gone
    // @ts-expect-error — passing cwd should be a type error now
    expect(() => resolveConfigPath("/some/cwd")).not.toThrow();
    const expected = join(homedir(), ".config", "duo", "config.yaml");
    // @ts-expect-error — passing cwd should be a type error now
    expect(resolveConfigPath("/some/cwd")).toBe(expected);
  });
});

describe("loadConfig", () => {
  const originalEnv = process.env;
  let tmp: string;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.DUO_CONFIG;
    delete process.env.XDG_CONFIG_HOME;
    delete process.env.DUO_POLICY;
    tmp = mkdtempSync(join(tmpdir(), "duo-config-loader-"));
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  it("returns minimum-viable defaults when config file does not exist", () => {
    process.env.DUO_CONFIG = join(tmp, "missing.yaml");
    const loaded = loadConfig({ cwd: tmp });
    expect(loaded.usedDefaults).toBe(true);
    expect(loaded.config.solo.transport.type).toBe("stdio");
    expect(loaded.config.solo.transport.command).toBeUndefined();
  });

  it("throws a clear error when config file exists but is empty", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "");
    process.env.DUO_CONFIG = path;
    expect(() => loadConfig({ cwd: tmp })).toThrow(/is empty/);
  });

  it("throws a clear error when config file is whitespace/comments only", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "# just a comment\n   \n");
    process.env.DUO_CONFIG = path;
    expect(() => loadConfig({ cwd: tmp })).toThrow(/is empty/);
  });

  it("throws a clear error when config file is an empty mapping ({})", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "{}\n");
    process.env.DUO_CONFIG = path;
    expect(() => loadConfig({ cwd: tmp })).toThrow(/is empty/);
  });

  it("loads a populated config file as usedDefaults=false", () => {
    const path = join(tmp, "config.yaml");
    writeFileSync(path, "solo:\n  transport:\n    type: stdio\n");
    process.env.DUO_CONFIG = path;
    const loaded = loadConfig({ cwd: tmp });
    expect(loaded.usedDefaults).toBe(false);
  });
});
