import { afterEach, describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { getGitSha, getVersion } from "./version-info.js";

const here = dirname(fileURLToPath(import.meta.url));
const pkg = JSON.parse(readFileSync(resolve(here, "../../package.json"), "utf8")) as {
  version: string;
};

const globalAsRecord = globalThis as Record<string, unknown>;

describe("getVersion", () => {
  afterEach(() => {
    delete globalAsRecord.__DUO_VERSION__;
  });

  it("returns the package.json version via the dev-mode walk when no compile-time version is injected", () => {
    expect(getVersion()).toBe(pkg.version);
  });

  it("returns the injected __DUO_VERSION__ global when present (compiled-binary path)", () => {
    globalAsRecord.__DUO_VERSION__ = "9.9.9-test";
    expect(getVersion()).toBe("9.9.9-test");
  });
});

describe("getGitSha", () => {
  it("returns a short sha when run inside a git checkout", () => {
    expect(getGitSha()).toMatch(/^[0-9a-f]{7,}$/);
  });
});
