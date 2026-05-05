import { afterEach, describe, it, expect } from "vitest";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
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
  afterEach(() => {
    delete globalAsRecord.__DUO_GIT_SHA__;
  });

  it("returns a short sha when run inside a git checkout (dev-mode fallback)", () => {
    expect(getGitSha()).toMatch(/^[0-9a-f]{7,}$/);
  });

  it("returns the injected __DUO_GIT_SHA__ global when present (build-time path)", () => {
    globalAsRecord.__DUO_GIT_SHA__ = "deadbeef";
    expect(getGitSha()).toBe("deadbeef");
  });

  it("returns undefined when run outside a git checkout and no SHA is injected", () => {
    const dir = mkdtempSync(join(tmpdir(), "duo-no-git-"));
    const orig = process.cwd();
    process.chdir(dir);
    try {
      expect(getGitSha()).toBeUndefined();
    } finally {
      process.chdir(orig);
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
