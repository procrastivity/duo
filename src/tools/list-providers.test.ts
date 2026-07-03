import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setProviderEnabled } from "../state/providers.js";
import { listProvidersHandler } from "./list-providers.js";

const parse = (result: { content: { text: string }[] }) =>
  JSON.parse(result.content[0].text);

// Drives the real `listProviders()` through the shared state seam (a temp
// XDG_STATE_HOME dir), matching src/state/providers.test.ts.
describe("list_providers tool", () => {
  const originalEnv = process.env;
  let tmp: string;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.XDG_STATE_HOME;
    tmp = mkdtempSync(join(tmpdir(), "duo-list-providers-"));
    process.env.XDG_STATE_HOME = tmp;
  });

  afterEach(() => {
    process.env = originalEnv;
    rmSync(tmp, { recursive: true, force: true });
  });

  it("returns an empty provider list when no state exists", () => {
    const result = listProvidersHandler();
    expect(result.isError).toBeFalsy();
    expect(parse(result)).toEqual({ providers: [] });
  });

  it("reflects listProviders() output with enabled/disabled status, sorted", () => {
    setProviderEnabled("openrouter", true);
    setProviderEnabled("anthropic", false);
    setProviderEnabled("openai", false);
    const result = listProvidersHandler();
    expect(result.isError).toBeFalsy();
    expect(parse(result)).toEqual({
      providers: [
        { provider: "anthropic", enabled: false },
        { provider: "openai", enabled: false },
        { provider: "openrouter", enabled: true },
      ],
    });
  });
});
