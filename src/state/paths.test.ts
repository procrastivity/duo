import { homedir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  assertValidProviderLabel,
  isValidProviderLabel,
  resolveProviderStateDir,
} from "./paths.js";

describe("resolveProviderStateDir", () => {
  const originalEnv = process.env;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.XDG_STATE_HOME;
  });

  afterEach(() => {
    process.env = originalEnv;
  });

  it("returns $XDG_STATE_HOME/duo/providers when XDG_STATE_HOME is set", () => {
    process.env.XDG_STATE_HOME = "/custom/state";
    expect(resolveProviderStateDir()).toBe("/custom/state/duo/providers");
  });

  it("falls back to ~/.local/state/duo/providers when XDG_STATE_HOME is unset", () => {
    const expected = join(homedir(), ".local", "state", "duo", "providers");
    expect(resolveProviderStateDir()).toBe(expected);
  });
});

describe("provider label validation", () => {
  const valid = ["openrouter", "openai", "anthropic", "a.b-c_1", "A", "1", "z_9"];
  const invalid = [
    "", // empty
    ".", // current dir
    "..", // parent dir (traversal)
    "a/b", // path separator
    "../escape", // traversal
    "foo/", // trailing separator
    "/foo", // leading separator
    "a b", // whitespace
    "a\tb", // tab
    "a\nb", // newline
    "café", // non-ASCII (outside charset)
    "a\\b", // backslash separator
    "*", // glob char
    "provider!", // punctuation outside charset
  ];

  for (const label of valid) {
    it(`accepts ${JSON.stringify(label)}`, () => {
      expect(isValidProviderLabel(label)).toBe(true);
      expect(() => assertValidProviderLabel(label)).not.toThrow();
    });
  }

  for (const label of invalid) {
    it(`rejects ${JSON.stringify(label)}`, () => {
      expect(isValidProviderLabel(label)).toBe(false);
      expect(() => assertValidProviderLabel(label)).toThrow();
    });
  }
});
