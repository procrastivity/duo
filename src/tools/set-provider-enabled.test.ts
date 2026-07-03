import {
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { resolveProviderStateDir } from "../state/paths.js";
import { isProviderEnabled } from "../state/providers.js";
import { setProviderEnabledHandler } from "./set-provider-enabled.js";

const parse = (result: { content: { text: string }[] }) =>
  JSON.parse(result.content[0].text);

// Asserts against real provider state through the shared seam (a temp
// XDG_STATE_HOME dir), matching src/state/providers.test.ts.
describe("set_provider_enabled tool", () => {
  const originalEnv = process.env;
  let tmp: string;
  let providersDir: string;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.XDG_STATE_HOME;
    tmp = mkdtempSync(join(tmpdir(), "duo-set-provider-"));
    process.env.XDG_STATE_HOME = tmp;
    providersDir = resolveProviderStateDir();
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  it("disables a provider and writes '0' to state", () => {
    const result = setProviderEnabledHandler({ provider: "openai", enabled: false });
    expect(result.isError).toBeFalsy();
    expect(parse(result)).toEqual({ provider: "openai", enabled: false });
    expect(isProviderEnabled("openai")).toBe(false);
    expect(readFileSync(join(providersDir, "openai"), "utf8")).toBe("0");
  });

  it("enables a provider and writes '1' to state", () => {
    const result = setProviderEnabledHandler({ provider: "openai", enabled: true });
    expect(result.isError).toBeFalsy();
    expect(parse(result)).toEqual({ provider: "openai", enabled: true });
    expect(isProviderEnabled("openai")).toBe(true);
    expect(readFileSync(join(providersDir, "openai"), "utf8")).toBe("1");
  });

  it("rejects an invalid label with a structured error and writes no state", () => {
    const result = setProviderEnabledHandler({ provider: "../escape", enabled: true });
    expect(result.isError).toBe(true);
    const payload = parse(result);
    expect(payload.code).toBe("invalid_provider_label");
    expect(payload.provider).toBe("../escape");
    // The invalid label throws before any filesystem access: no dir, no leaked file.
    expect(existsSync(providersDir)).toBe(false);
    expect(existsSync(join(tmp, "duo", "escape"))).toBe(false);
  });
});
