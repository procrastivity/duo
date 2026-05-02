# Step 1 Workplan — Project Scaffold and Solo Connection

**Status**: shipped  
**Roadmap**: `notes/roadmap/roadmap-1.md`  
**Intake**: `notes/proposals/solo-orchestrator-companion-intake.md`  
**Source coverage**: PRD REQ-008, REQ-011 (detection half); Stories 10, 11 (detection half)

---

## Scope

- **Goal**: TypeScript MCP server skeleton that starts, connects to Solo via stdio command-spawn, validates config at startup, and runs a vitest suite with injectable Solo client.
- **Out of scope**: tier classification, resolver logic, MCP tools beyond startup health, any spawn behavior.

---

## Tasks

### Task 1 — TypeScript project initialization

Initialize the project skeleton: `package.json`, `tsconfig.json`, `vitest.config.ts`. Install all Step 1 deps (`@modelcontextprotocol/sdk`, `zod`, `vitest`, `yaml`, `execa`).

**Files**: `package.json`, `tsconfig.json`, `vitest.config.ts`  
**Tests**: `npm test` (vitest) runs and exits zero with no test files yet; basic smoke test file passes.

---

### Task 2 — Config schema (`src/config.ts`)

Define and validate the Solo connection config with `zod`. Must cover the stdio command-spawn transport. Include `SOLO_PROCESS_ID` and `SOLO_PROJECT_ID` env-var detection here or in Task 6 (either is fine; keep consistent).

**Files**: `src/config.ts`  
**Tests**:
- Valid config parses without error
- Missing required field throws with field-level message
- Invalid field type throws with field-level message

---

### Task 3 — Solo transport abstraction (`src/transport/`)

Define a transport interface type (so a future HTTP transport can implement it without touching the client). Implement the stdio command-spawn transport using `execa`.

**Files**: `src/transport/types.ts`, `src/transport/stdio.ts`  
**Tests**:
- A test double can satisfy the transport interface
- Stdio transport can be constructed with valid config (no live process needed for unit test)

---

### Task 4 — Solo MCP client (`src/solo-client.ts`)

Wrap the transport in a Solo MCP client. Implement `listAgentTools` as the first live method (needed by Step 2). Client must accept a transport via constructor injection.

**Files**: `src/solo-client.ts`  
**Tests**:
- Client instantiates with mock transport
- `listAgentTools` returns parsed response when mock transport returns valid payload
- `listAgentTools` throws structured error when mock transport returns an error

---

### Task 5 — MCP server bootstrap (`src/server.ts`, `src/index.ts`)

Register the MCP server with `@modelcontextprotocol/sdk`. Validate config at startup; exit with a structured error if config is invalid or Solo connection fails to initialize.

**Files**: `src/server.ts`, `src/index.ts`  
**Tests**:
- Server instantiates with valid config
- Server startup with invalid config rejects with structured error (not an unhandled exception)

---

### Task 6 — Environment detection (`src/env.ts`)

Detect `SOLO_PROCESS_ID` and `SOLO_PROJECT_ID` from environment. Surface them in session context (informational only — no behavioral effect in MVP).

**Files**: `src/env.ts`  
**Tests**:
- Values returned when env vars are set
- Values absent (undefined/null) when env vars are not set
- No side effects from detection alone

---

## Deferred Decisions Resolved Here

- **Transport mode → stdio command-spawn**  
  Lowest setup friction; MCP SDK makes stdio well-supported. Abstraction layer lets HTTP be added later without rewriting the client.  
  Source: PRD Open Question 4 (resolved in intake).

- **Config validation → at startup, not on first call**  
  Fail-fast; operator gets clear feedback before any tool call is attempted.  
  Source: Story 10 AC; intake resolution.

---

## Definition of Done

- [x] `npm test` (vitest) runs and passes — **VERIFIED: 19/19 tests passing**
- [x] `npm start` with valid config starts the MCP server without error
- [x] `npm start` with invalid/missing config exits with a clear structured error message
- [x] Transport layer has an interface type that a future HTTP transport could implement
- [x] `SOLO_PROCESS_ID` and `SOLO_PROJECT_ID` detected and available in session context (informational only)

---

## Suggested Build Batching

| Batch | Tasks | Notes |
|---|---|---|
| Batch A | Task 1, Task 2, Task 6 | No inter-task deps; pure scaffold and config |
| Batch B | Task 3, Task 4 | Transport before client; sequential within batch |
| Batch C | Task 5 | Server wires everything together; depends on A+B |

Batches A and B can overlap if two builders are available; Batch C is a gate.
