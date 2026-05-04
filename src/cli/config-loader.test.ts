import { homedir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { resolveConfigPath } from "./config-loader.js";

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
    // Even if a spurious arg is passed JS won't fail, but the result
    // should still be the XDG fallback (arg is ignored)
    const expected = join(homedir(), ".config", "duo", "config.yaml");
    // @ts-expect-error — passing cwd should be a type error now
    expect(resolveConfigPath("/some/cwd")).toBe(expected);
  });
});
