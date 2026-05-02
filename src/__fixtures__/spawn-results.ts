// Purpose: Solo raw spawn-process response when caller supplied name="my-helper".
// Used by: Task 3 spawn_agent tool tests (named-spawn passthrough).
// Disabled-id note: agent_tool_id 2 (opencode-ghc-sonnet) is enabled in enabledRuntimes and mixedRealistic.
export const spawnSuccessNamed = {
  process_id: "proc-aaa-111",
  name: "my-helper",
  agent_tool_id: 2,
  project_id: "proj-default-001",
};

// Purpose: Solo raw spawn-process response when caller omitted name; Solo generated "agent-1234".
// Used by: Task 3 spawn_agent tool tests (unnamed-spawn passthrough — name comes from Solo, not caller).
// Disabled-id note: agent_tool_id 2 (opencode-ghc-sonnet) is enabled in enabledRuntimes and mixedRealistic.
export const spawnSuccessUnnamed = {
  process_id: "proc-bbb-222",
  name: "agent-1234",
  agent_tool_id: 2,
  project_id: "proj-default-001",
};

// Purpose: Solo raw spawn-process response when caller supplied project_id="proj-caller-abc".
// The fixture's project_id equals the caller-supplied value, asserting caller precedence over env.
// Used by: Task 3 spawn_agent tool tests (caller project_id precedence over SOLO_PROJECT_ID env).
// Disabled-id note: agent_tool_id 2 (opencode-ghc-sonnet) is enabled in enabledRuntimes and mixedRealistic.
export const spawnSuccessWithProjectId = {
  process_id: "proc-ccc-333",
  name: "my-helper",
  agent_tool_id: 2,
  project_id: "proj-caller-abc",
};

// Purpose: Solo raw spawn-process response when caller omitted project_id and SOLO_PROJECT_ID="proj-env-xyz" was used.
// The fixture's project_id equals the env value, asserting env fallback when caller omits project_id.
// Used by: Task 3 spawn_agent tool tests (SOLO_PROJECT_ID env fallback path).
// Disabled-id note: agent_tool_id 2 (opencode-ghc-sonnet) is enabled in enabledRuntimes and mixedRealistic.
export const spawnSuccessFromEnvProjectId = {
  process_id: "proc-ddd-444",
  name: "agent-5678",
  agent_tool_id: 2,
  project_id: "proj-env-xyz",
};

// Purpose: Solo JSON-RPC error envelope when a requested spawn name is already in use.
// Drives the name-rejection passthrough test — tool must surface this verbatim under spawn_rejected.
// Used by: Task 3 spawn_agent tool tests (name-in-use rejection path).
export const spawnRejectionNameInUse = {
  code: -32602,
  message: "name 'my-helper' already in use",
};

// Purpose: Solo JSON-RPC error envelope when the supplied agent_tool_id does not exist on Solo's side.
// Verifies the tool surfaces the error verbatim rather than masking it.
// Used by: Task 3 spawn_agent tool tests (invalid agent_tool_id rejection path).
export const spawnRejectionInvalidAgentToolId = {
  code: -32602,
  message: "agent_tool_id 999 does not exist",
};

// Purpose: Solo JSON-RPC error envelope when the caller lacks access to the requested project scope.
// Verifies project-scope errors surface cleanly under spawn_rejected.
// Used by: Task 3 spawn_agent tool tests (permission-denied rejection path).
export const spawnRejectionPermissionDenied = {
  code: -32603,
  message: "permission denied: project 'proj-other' is not accessible",
};

// Purpose: tools/call response payload with process_id absent — simulates Solo contract drift.
// Zod parse of this object must throw, exercising the malformed-payload error path.
// Used by: Task 3 spawn_agent tool tests (zod parse error / contract-drift path).
// Disabled-id note: agent_tool_id 2 referenced but process_id is intentionally missing to force parse failure.
export const spawnMalformedPayload = {
  name: "agent-9999",
  agent_tool_id: 2,
  project_id: "proj-default-001",
  // process_id intentionally absent
};
