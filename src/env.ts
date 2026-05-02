export type SoloEnvContext = {
  soloProcessId?: string;
  soloProjectId?: string;
};

type EnvSource = Record<string, string | undefined>;

export const detectSoloEnv = (env: EnvSource = process.env): SoloEnvContext => {
  const soloProcessId = env.SOLO_PROCESS_ID;
  const soloProjectId = env.SOLO_PROJECT_ID;

  return {
    soloProcessId,
    soloProjectId,
  };
};
