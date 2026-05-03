import { describe, expect, it } from "vitest";
import {
  longestPathMatch,
  resolveProjectIdAtConnect,
  resolveProcessIdFromEnv,
} from "./scope.js";
import type { SoloProject } from "../types/solo.js";

const projects: SoloProject[] = [
  { id: 1, name: "outer", path: "/Users/me/Code" },
  { id: 2, name: "duo", path: "/Users/me/Code/duo" },
  { id: 3, name: "other", path: "/Users/me/elsewhere" },
];

describe("longestPathMatch", () => {
  it("picks the longest path match", () => {
    expect(longestPathMatch(projects, "/Users/me/Code/duo/src")?.id).toBe(2);
  });

  it("matches exact path", () => {
    expect(longestPathMatch(projects, "/Users/me/Code/duo")?.id).toBe(2);
  });

  it("does not match a parent of the project path", () => {
    expect(longestPathMatch(projects, "/Users/me")?.id).toBeUndefined();
  });

  it("does not match siblings due to prefix collision", () => {
    expect(longestPathMatch(projects, "/Users/me/Code/duo-sibling")?.id).toBe(1);
    // "/Users/me/Code/duo-sibling" must NOT match "/Users/me/Code/duo" — guarded by path+"/"
  });

  it("returns undefined when nothing matches", () => {
    expect(longestPathMatch(projects, "/tmp/x")).toBeUndefined();
  });
});

describe("resolveProjectIdAtConnect", () => {
  it("env wins over pwd when both resolve", () => {
    const r = resolveProjectIdAtConnect(
      { SOLO_PROJECT_ID: "99" },
      "/Users/me/Code/duo",
      projects,
    );
    expect(r.projectId).toBe(99);
    expect(r.envProjectId).toBe(99);
    expect(r.pwdProjectId).toBe(2);
  });

  it("falls back to pwd when env unset", () => {
    const r = resolveProjectIdAtConnect({}, "/Users/me/Code/duo/x", projects);
    expect(r.projectId).toBe(2);
  });

  it("undefined when env unset and pwd unmatched", () => {
    const r = resolveProjectIdAtConnect({}, "/tmp/x", projects);
    expect(r.projectId).toBeUndefined();
  });

  it("ignores non-integer SOLO_PROJECT_ID", () => {
    const r = resolveProjectIdAtConnect(
      { SOLO_PROJECT_ID: "not-a-number" },
      "/Users/me/Code/duo",
      projects,
    );
    expect(r.envProjectId).toBeUndefined();
    expect(r.projectId).toBe(2);
  });
});

describe("resolveProcessIdFromEnv", () => {
  it("parses integer", () => {
    expect(resolveProcessIdFromEnv({ SOLO_PROCESS_ID: "297" })).toBe(297);
  });

  it("returns undefined when unset", () => {
    expect(resolveProcessIdFromEnv({})).toBeUndefined();
  });

  it("returns undefined when non-integer", () => {
    expect(resolveProcessIdFromEnv({ SOLO_PROCESS_ID: "abc" })).toBeUndefined();
  });
});
