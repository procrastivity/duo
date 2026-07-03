import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { resolveProviderStateDir } from "./paths.js";
import { isProviderEnabled, listProviders, setProviderEnabled } from "./providers.js";

describe("provider state", () => {
  const originalEnv = process.env;
  let tmp: string;
  let providersDir: string;

  // Write a provider state file directly, bypassing setProviderEnabled.
  const write = (provider: string, content: string) => {
    mkdirSync(providersDir, { recursive: true });
    writeFileSync(join(providersDir, provider), content);
  };

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.XDG_STATE_HOME;
    tmp = mkdtempSync(join(tmpdir(), "duo-provider-state-"));
    process.env.XDG_STATE_HOME = tmp;
    providersDir = resolveProviderStateDir();
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  describe("isProviderEnabled semantics", () => {
    it("content '0' ⇒ disabled", () => {
      write("openai", "0");
      expect(isProviderEnabled("openai")).toBe(false);
    });

    it("absent provider ⇒ enabled (opt-out default)", () => {
      write("other", "0"); // dir exists, but this label is absent
      expect(isProviderEnabled("never-written")).toBe(true);
    });

    it("missing dir ⇒ enabled", () => {
      expect(existsSync(providersDir)).toBe(false);
      expect(isProviderEnabled("openai")).toBe(true);
    });

    it("content '1' ⇒ enabled", () => {
      write("openrouter", "1");
      expect(isProviderEnabled("openrouter")).toBe(true);
    });

    it("trailing-newline '0\\n' trims to disabled", () => {
      write("openai", "0\n");
      expect(isProviderEnabled("openai")).toBe(false);
    });

    it("surrounding whitespace '  0  ' trims to disabled", () => {
      write("openai", "  0  ");
      expect(isProviderEnabled("openai")).toBe(false);
    });

    it("non-exact '0x' ⇒ enabled", () => {
      write("openai", "0x");
      expect(isProviderEnabled("openai")).toBe(true);
    });

    it("arbitrary content ⇒ enabled", () => {
      write("openai", "enabled");
      expect(isProviderEnabled("openai")).toBe(true);
    });
  });

  describe("setProviderEnabled", () => {
    it("round-trips disable then enable", () => {
      setProviderEnabled("anthropic", false);
      expect(isProviderEnabled("anthropic")).toBe(false);
      setProviderEnabled("anthropic", true);
      expect(isProviderEnabled("anthropic")).toBe(true);
    });

    it("disable writes '0', enable writes '1'", () => {
      setProviderEnabled("openai", false);
      expect(readFileSync(join(providersDir, "openai"), "utf8")).toBe("0");
      setProviderEnabled("openai", true);
      expect(readFileSync(join(providersDir, "openai"), "utf8")).toBe("1");
    });

    it("auto-creates the state dir when absent", () => {
      expect(existsSync(providersDir)).toBe(false);
      setProviderEnabled("openai", false);
      expect(existsSync(providersDir)).toBe(true);
    });

    it("leaves only the target file(s) — atomic write, no stray *.tmp", () => {
      setProviderEnabled("openai", false);
      setProviderEnabled("openai", true);
      setProviderEnabled("openrouter", false);
      const entries = readdirSync(providersDir).sort();
      expect(entries).toEqual(["openai", "openrouter"]);
      expect(entries.some((e) => e.endsWith(".tmp"))).toBe(false);
    });

    it("validates the label before any filesystem write", () => {
      expect(() => setProviderEnabled("../escape", true)).toThrow();
      // No file leaked outside the providers dir.
      expect(existsSync(join(tmp, "duo", "escape"))).toBe(false);
    });
  });

  describe("fresh reads (no caching)", () => {
    it("isProviderEnabled reflects a file mutated underneath, one process", () => {
      setProviderEnabled("openai", false);
      expect(isProviderEnabled("openai")).toBe(false);
      // Mutate the file directly underneath the reader.
      writeFileSync(join(providersDir, "openai"), "1");
      expect(isProviderEnabled("openai")).toBe(true);
    });

    it("listProviders reflects fresh directory state", () => {
      expect(listProviders()).toEqual([]);
      setProviderEnabled("openai", false);
      expect(listProviders()).toEqual([{ provider: "openai", enabled: false }]);
    });
  });

  describe("listProviders", () => {
    it("returns [] for a missing dir", () => {
      expect(existsSync(providersDir)).toBe(false);
      expect(listProviders()).toEqual([]);
    });

    it("returns [] for an empty dir", () => {
      mkdirSync(providersDir, { recursive: true });
      expect(listProviders()).toEqual([]);
    });

    it("returns providers sorted with correct enabled flags", () => {
      setProviderEnabled("openrouter", true);
      setProviderEnabled("anthropic", false);
      setProviderEnabled("openai", false);
      write("zeta", "1");
      expect(listProviders()).toEqual([
        { provider: "anthropic", enabled: false },
        { provider: "openai", enabled: false },
        { provider: "openrouter", enabled: true },
        { provider: "zeta", enabled: true },
      ]);
    });

    it("ignores invalid labels and transient temp files", () => {
      setProviderEnabled("openai", false);
      write("provider!", "0");
      write("openrouter.123.tmp", "0");

      expect(listProviders()).toEqual([{ provider: "openai", enabled: false }]);
    });
  });
});
