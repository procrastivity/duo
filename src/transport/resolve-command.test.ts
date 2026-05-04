import { existsSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { KNOWN_SOLO_PATHS, resolveTransportCommand } from "./resolve-command.js";

vi.mock("node:fs", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:fs")>();
  return { ...actual, existsSync: vi.fn() };
});

const mockExistsSync = vi.mocked(existsSync);

describe("resolveTransportCommand", () => {
  beforeEach(() => {
    mockExistsSync.mockReturnValue(false);
  });

  afterEach(() => {
    vi.resetAllMocks();
  });

  it("returns the configured command when provided", () => {
    const result = resolveTransportCommand("/custom/path/to/mcp");
    expect(result).toBe("/custom/path/to/mcp");
    // Should not call existsSync when a configured path is given
    expect(mockExistsSync).not.toHaveBeenCalled();
  });

  it("auto-detects the macOS Solo path when it exists", () => {
    mockExistsSync.mockImplementation((p) => p === "/Applications/Solo.app/Contents/MacOS/mcp");

    const result = resolveTransportCommand();
    expect(result).toBe("/Applications/Solo.app/Contents/MacOS/mcp");
  });

  it("returns the first known path that exists when multiple might match", () => {
    // All known paths exist — should return the first one
    mockExistsSync.mockReturnValue(true);

    const result = resolveTransportCommand();
    expect(result).toBe(KNOWN_SOLO_PATHS[0]);
  });

  it("throws a descriptive error when no path is found and nothing is configured", () => {
    mockExistsSync.mockReturnValue(false);

    expect(() => resolveTransportCommand()).toThrow(
      /Solo MCP binary not found/,
    );
  });

  it("error message lists searched paths", () => {
    mockExistsSync.mockReturnValue(false);

    expect(() => resolveTransportCommand()).toThrow(
      /\/Applications\/Solo\.app\/Contents\/MacOS\/mcp/,
    );
  });

  it("error message includes guidance on how to fix", () => {
    mockExistsSync.mockReturnValue(false);

    expect(() => resolveTransportCommand()).toThrow(
      /solo\.transport\.command/,
    );
  });

  it("does not use auto-detection when configured path is provided (even if empty string is not truthy)", () => {
    // undefined → auto-detect; empty string is falsy → also auto-detect
    mockExistsSync.mockImplementation((p) => p === "/Applications/Solo.app/Contents/MacOS/mcp");

    const result = resolveTransportCommand(undefined);
    expect(result).toBe("/Applications/Solo.app/Contents/MacOS/mcp");
  });
});
