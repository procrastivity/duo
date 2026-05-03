import type { SoloProject } from "../types/solo.js";

type EnvSource = Record<string, string | undefined>;

export interface ResolvedScope {
  projectId?: number;
  processId?: number;
  envProjectId?: number;
  pwdProjectId?: number;
}

const parseId = (raw: string | undefined): number | undefined => {
  if (raw === undefined || raw === "") return undefined;
  const n = Number(raw);
  return Number.isInteger(n) && n >= 0 ? n : undefined;
};

export const longestPathMatch = (
  projects: SoloProject[],
  cwd: string,
): SoloProject | undefined => {
  const matches = projects.filter((p) => cwd === p.path || cwd.startsWith(p.path + "/"));
  matches.sort((a, b) => b.path.length - a.path.length);
  return matches[0];
};

export const resolveProjectIdAtConnect = (
  env: EnvSource,
  cwd: string,
  projects: SoloProject[],
): { projectId?: number; envProjectId?: number; pwdProjectId?: number } => {
  const envProjectId = parseId(env.SOLO_PROJECT_ID);
  const pwdMatch = longestPathMatch(projects, cwd);
  const pwdProjectId = pwdMatch?.id;

  return {
    envProjectId,
    pwdProjectId,
    projectId: envProjectId ?? pwdProjectId,
  };
};

export const resolveProcessIdFromEnv = (env: EnvSource): number | undefined =>
  parseId(env.SOLO_PROCESS_ID);
