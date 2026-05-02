# Solo Orchestrator Companion — User Stories

**PRD**: [`notes/proposals/solo-orchestrator-companion-prd.md`](./solo-orchestrator-companion-prd.md)  
**Status**: Draft

These stories define delivery scope for a standalone TypeScript MCP companion to Solo. They do not prescribe the internal implementation beyond the PRD's global invariants.

---

## Global Invariants

- The companion never requires users to maintain static `agent_tool_id` mappings.
- Disabled Solo tools are excluded before tier classification.
- Classification uses `command` as the primary signal and `name` only as fallback.
- Resolver output is deterministic for identical inputs.
- Solo remains the authority for process creation.
- Unknown or unclassified tiers fail loudly unless an explicit configured fallback exists.

---

## Epic: Tier Discovery

### Story 1: List supported agent tiers

**As a playbook author, I want to list the agent tiers available in the local Solo environment, so that my workflow can adapt before trying to delegate work.**

**Acceptance Criteria**:

- [ ] Calling `list_agent_tiers` returns the supported tier labels, including `small`, `medium`, and `large`.
- [ ] Each returned tier includes whether it is currently available from enabled Solo tools.
- [ ] Each available tier includes the selected default candidate's Solo tool id, name, command summary, and tool type.
- [ ] Each tier includes alternatives when more than one enabled candidate matches.
- [ ] If Solo is unreachable, the tool returns a structured error that identifies the Solo connection problem and does not return fabricated tier availability.

### Story 2: Exclude disabled tools from tier availability

**As a runtime maintainer, I want disabled Solo tools to be ignored by tier discovery, so that retired or experimental runtimes are not accidentally used by orchestration workflows.**

**Acceptance Criteria**:

- [ ] Given a Solo tool list with an enabled `large` candidate and a disabled `large` candidate, `list_agent_tiers` reports only the enabled candidate.
- [ ] Given a Solo tool list where the only `small` candidate is disabled, `list_agent_tiers` reports `small` as unavailable.
- [ ] Resolver diagnostics state how many tools were ignored because `enabled != true`.

---

## Epic: Tier Resolution

### Story 3: Resolve a tier to a Solo agent tool

**As a playbook author, I want to resolve a capability tier to the concrete Solo agent tool that will be used, so that I can audit delegation choices without spawning a process.**

**Acceptance Criteria**:

- [ ] Calling `resolve_agent_tool` with `tier: "small"` returns exactly one selected candidate when an enabled small candidate exists.
- [ ] The response includes the selected candidate's `agent_tool_id`, `name`, `command`, `tool_type`, matched tier, and classification signals.
- [ ] The response includes alternatives in deterministic order when more than one enabled candidate matches.
- [ ] Repeating the same request against the same Solo tool list returns the same selected candidate and alternatives order.

### Story 4: Classify by command before name

**As a runtime maintainer, I want classification to prefer command tokens over display names, so that I can give agents readable names without changing orchestration behavior.**

**Acceptance Criteria**:

- [ ] Given a tool named `codex-flagship` whose command contains a small-model token, resolver classifies it according to the command token, not the name.
- [ ] Given a tool whose name suggests `small` but whose command contains a large-model token, resolver classifies it according to the command token.
- [ ] Given a tool with no recognizable command token but a recognizable name token, resolver may classify by name and marks the match source as `name_fallback`.
- [ ] Resolver diagnostics expose whether the selected candidate matched by command or by name fallback.

### Story 5: Fail loudly when no candidate exists

**As an agent operator, I want missing tier matches to return clear errors, so that workflows do not silently spawn the wrong capability level.**

**Acceptance Criteria**:

- [ ] Calling `resolve_agent_tool` with an unsupported tier label returns a structured `unsupported_tier` error.
- [ ] Calling `resolve_agent_tool` for a supported tier with no enabled candidates returns a structured `tier_unavailable` error.
- [ ] The error response includes the requested tier and a list of currently available tiers.
- [ ] No response for a no-match case includes an `agent_tool_id`.

### Story 6: Apply deterministic candidate ranking

**As a playbook author, I want repeated tier resolution to choose the same candidate for the same input, so that orchestration behavior is predictable.**

**Acceptance Criteria**:

- [ ] Given multiple enabled candidates matching `medium`, the selected candidate is stable across repeated resolver calls.
- [ ] Candidate alternatives are returned in the same order across repeated resolver calls.
- [ ] The resolver response includes enough ranking diagnostics to explain why the selected candidate won.
- [ ] A fixture test with the current observed Solo tools resolves each populated tier deterministically.

---

## Epic: Tier-Based Spawning

### Story 7: Spawn an agent by tier

**As a playbook author, I want to spawn a Solo-managed agent by capability tier, so that my workflow can delegate work without hard-coding local Solo tool IDs.**

**Acceptance Criteria**:

- [ ] Calling `spawn_agent` with `tier: "large"` resolves the tier, then delegates to Solo `spawn_process` with `kind: "agent"` and the selected `agent_tool_id`.
- [ ] The response includes Solo's returned process id and process name.
- [ ] The response includes the selected tier and selected Solo tool summary.
- [ ] If Solo rejects the spawn request, `spawn_agent` returns a structured error containing Solo's failure message and does not report success.

### Story 8: Pass through human-friendly process names

**As an agent operator, I want to provide a readable name for a spawned agent, so that I can identify it in Solo process views.**

**Acceptance Criteria**:

- [ ] Calling `spawn_agent` with `name: "research-large"` passes that name to Solo's spawn request.
- [ ] The response returns the final process name reported by Solo.
- [ ] If the caller omits `name`, the companion allows Solo to generate or normalize the process name.
- [ ] If Solo rejects a provided name, the companion returns a structured validation or spawn error instead of retrying with a different hidden name.

### Story 9: Respect project scope

**As a playbook author, I want spawned agents to land in the intended Solo project, so that orchestration does not leak work into the wrong project context.**

**Acceptance Criteria**:

- [ ] When the caller supplies an explicit project id, `spawn_agent` passes that project id to Solo.
- [ ] When no project id is supplied and `SOLO_PROJECT_ID` is available, the companion may use it as the default project scope.
- [ ] When no project id is available from input or environment, the companion relies on Solo's effective session project or returns a structured configuration error, depending on the configured connection mode.
- [ ] The response includes the project id used when Solo returns it or when the companion supplied it.

---

## Epic: Configuration & Operations

### Story 10: Configure Solo connection explicitly

**As an agent operator, I want to configure how the companion connects to Solo, so that the companion can run outside a Solo-managed process.**

**Acceptance Criteria**:

- [ ] The companion accepts explicit Solo connection configuration at startup.
- [ ] Invalid connection configuration fails startup with a clear error.
- [ ] Missing connection configuration does not silently assume a transport endpoint that cannot be verified.
- [ ] Documentation shows explicit configuration as the primary setup path.

### Story 11: Use Solo environment context as a convenience

**As an agent operator, I want the companion to use Solo environment variables when available, so that setup is simpler inside Solo-managed agent sessions.**

**Acceptance Criteria**:

- [ ] When `SOLO_PROJECT_ID` is present, resolver or spawn diagnostics include that it was detected as project context.
- [ ] When `SOLO_PROCESS_ID` is present, diagnostics include that it was detected as caller process context.
- [ ] Environment detection does not override an explicit caller-supplied project id.
- [ ] If environment variables are absent, core tools still work when explicit Solo connection configuration is valid.

### Story 12: Override tier policy locally

**As a runtime maintainer, I want to define local tier classification and ranking rules, so that custom Solo agent tools can participate without modifying companion source code.**

**Acceptance Criteria**:

- [ ] A YAML policy can add or adjust command-token patterns for `small`, `medium`, and `large`.
- [ ] A YAML policy can define preference ordering when multiple candidates match the same tier.
- [ ] Invalid YAML policy fails validation with specific field-level errors.
- [ ] Resolver diagnostics identify when a candidate matched a configured override rather than a built-in rule.

### Story 13: Emit structured operational logs

**As an agent operator, I want structured logs for resolution and spawn decisions, so that I can debug unexpected delegation behavior.**

**Acceptance Criteria**:

- [ ] A successful resolution log includes requested tier, selected tool id, selected tool name, match source, and candidate count.
- [ ] A failed resolution log includes requested tier, error code, and available tier labels.
- [ ] A successful spawn log includes requested tier, selected tool id, Solo process id, and process name.
- [ ] Logs do not include full prompts or sensitive free-form task content in MVP.

---

## Epic: Documentation & Adoption

### Story 14: Document agent/playbook usage

**As a playbook author, I want concise examples of the companion MCP tools, so that I can update workflows to use tiered spawning correctly.**

**Acceptance Criteria**:

- [ ] Documentation includes examples for `list_agent_tiers`, `resolve_agent_tool`, and `spawn_agent`.
- [ ] Examples use tier labels instead of fixed `agent_tool_id` values.
- [ ] Documentation explicitly states that the companion is standalone and not a Hypomnema feature.
- [ ] Documentation states that direct Solo `spawn_process` remains available, but playbooks should prefer the companion for tier-based agent spawning.

### Story 15: Provide fixture coverage for observed Solo runtimes

**As a runtime maintainer, I want tests based on current Solo tool metadata, so that future classifier changes do not break known local behavior.**

**Acceptance Criteria**:

- [ ] Test fixtures include the observed enabled tools: `opencode-ghc-haiku`, `opencode-ghc-sonnet`, `codex-fast`, `codex-standard`, and `codex-flagship`.
- [ ] Fixture tests assert that enabled candidates are classified by command tokens before name tokens.
- [ ] Fixture tests include disabled variants of matching tools and assert they are ignored.
- [ ] Fixture tests include ambiguous and unknown command cases with expected structured diagnostics.

---

## Open Story-Splitting Notes

- If connection setup becomes complex, split Story 10 into one story per supported transport mode.
- If prompt/bootstrap forwarding is added to `spawn_agent`, add a separate story rather than expanding Story 7.
- If custom tiers are accepted in v0, split Story 12 into "custom classifier rules" and "custom tier labels."
