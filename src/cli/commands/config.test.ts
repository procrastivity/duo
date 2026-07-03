import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configCommand } from "./config.js";

// Invoke a `config provider <verb>` subcommand's run() directly, as other
// command tests do (there is no argv parsing here — args are passed verbatim).
const runProvider = (
  verb: "enable" | "disable" | "list",
  args: Record<string, unknown>,
) => {
  const provider = (configCommand.subCommands as any).provider;
  return provider.subCommands[verb].run({ args });
};

describe("config provider CLI", () => {
  const originalEnv = process.env;
  let tmp: string;
  let out: string[];
  let err: string[];
  let exitCode: number | null;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.XDG_STATE_HOME;
    tmp = mkdtempSync(join(tmpdir(), "duo-config-provider-"));
    process.env.XDG_STATE_HOME = tmp;

    out = [];
    err = [];
    exitCode = null;
    vi.spyOn(process.stdout, "write").mockImplementation((s: any) => {
      out.push(String(s));
      return true;
    });
    vi.spyOn(process.stderr, "write").mockImplementation((s: any) => {
      err.push(String(s));
      return true;
    });
    vi.spyOn(process, "exit").mockImplementation(((code?: number) => {
      exitCode = code ?? 0;
      throw new Error(`__exit_${code}`);
    }) as never);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  const providersFile = (label: string) =>
    join(tmp, "duo", "providers", label);

  it("disable X writes the state file and list shows X disabled", async () => {
    await runProvider("disable", { label: "openai" });
    expect(readFileSync(providersFile("openai"), "utf8")).toBe("0");

    out = [];
    await runProvider("list", {});
    const table = out.join("");
    expect(table).toContain("openai");
    expect(table).toContain("disabled");
  });

  it("enable flips a disabled provider back to enabled", async () => {
    await runProvider("disable", { label: "openai" });
    await runProvider("enable", { label: "openai" });
    expect(readFileSync(providersFile("openai"), "utf8")).toBe("1");

    out = [];
    await runProvider("list", { json: true });
    expect(JSON.parse(out.join(""))).toEqual([
      { provider: "openai", enabled: true },
    ]);
  });

  it("enable --json reports the provider and its new state", async () => {
    await runProvider("enable", { label: "openrouter", json: true });
    expect(JSON.parse(out.join(""))).toEqual({
      provider: "openrouter",
      enabled: true,
    });
  });

  it("list --json returns rows sorted by provider", async () => {
    await runProvider("disable", { label: "openrouter" });
    await runProvider("enable", { label: "anthropic" });
    out = [];
    await runProvider("list", { json: true });
    expect(JSON.parse(out.join(""))).toEqual([
      { provider: "anthropic", enabled: true },
      { provider: "openrouter", enabled: false },
    ]);
  });

  it("invalid label exits non-zero with a stderr message and writes nothing", async () => {
    await expect(runProvider("disable", { label: "../escape" })).rejects.toThrow();
    expect(exitCode).toBe(1);
    expect(err.join("")).toMatch(/Invalid provider label/);
    // Nothing leaked outside the providers dir.
    const { existsSync } = await import("node:fs");
    expect(existsSync(join(tmp, "duo", "escape"))).toBe(false);
  });
});
