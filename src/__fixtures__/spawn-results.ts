// Solo raw spawn-process responses. Note: process_id and project_id are integers
// (Solo's i64), not strings. Solo's actual response includes process_id, name, and
// agent_instructions; agent_tool_id and project_id are not present in the result.
// Our SoloSpawnResultSchema is passthrough on the rest.
export const spawnSuccessNamed = {
  process_id: 111,
  name: "my-helper",
  agent_tool_id: 2,
  project_id: 1,
};

export const spawnSuccessUnnamed = {
  process_id: 222,
  name: "agent-1234",
  agent_tool_id: 2,
  project_id: 1,
};

export const spawnSuccessWithProjectId = {
  process_id: 333,
  name: "my-helper",
  agent_tool_id: 2,
  project_id: 7,
};

export const spawnSuccessFromEnvProjectId = {
  process_id: 444,
  name: "agent-5678",
  agent_tool_id: 2,
  project_id: 6,
};

export const spawnRejectionNameInUse = {
  code: -32602,
  message: "name 'my-helper' already in use",
};

export const spawnRejectionInvalidAgentToolId = {
  code: -32602,
  message: "agent_tool_id 999 does not exist",
};

export const spawnRejectionPermissionDenied = {
  code: -32603,
  message: "permission denied: project 99 is not accessible",
};

// process_id intentionally absent → forces parse failure for contract-drift path.
export const spawnMalformedPayload = {
  name: "agent-9999",
  agent_tool_id: 2,
  project_id: 1,
};
