package registry

// This file is data: the descriptor table, the deliberately absent rows,
// the stable error-code set, and the permission vocabulary. Rows cite the
// planning document that attests
// them; docs/registry/decisions.md records the row policy — an operation
// the planning set names joins the table only when the synced contract set
// backs it with a resolvable schema reference or fixture.

// externalV1 is the wire-truth envelope schema every public operation's
// requests and results decode under.
var externalV1 = SchemaRef{
	Family: "duo.external/v1",
	File:   "schemas/duo-external-v1.schema.json",
}

// externalDef references a named definition under duo.external/v1's $defs.
func externalDef(def string) SchemaRef {
	r := externalV1
	r.Def = def
	return r
}

// manifestV1 is the static manifest projection schema, the result shape of
// manifest.show.
var manifestV1 = SchemaRef{
	Family: "duo.manifest/v1",
	File:   "schemas/duo-manifest-v1.schema.json",
}

// table is the registry. Milestone rows first (the dogfood set, handoff 20),
// then the contract-attested remainder of the v1 surface as data.
var table = []Descriptor{
	// --- Dogfood milestone operations ---
	{
		Name:           "session.launch",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalDef("launch_resolution_report"),
		Permissions:    []string{"session.manage"},
		Idempotency:    IdempotencyRequired,
		Audit:          AuditPrivilegedWrite,
		CLI:            []string{"session", "launch"},
		// local_admin: absent from the initial generated MCP tool set — an
		// in-session agent that can launch more agents is a
		// self-activation and resource-amplification loop
		// (duo-vnext-public-contract.md §6.1, G-12 analogue).
		MCPTool: "",
		Route:   nil,
		ErrorCodes: []string{
			"preset.not_found",
			"launch.constraints_exhausted",
			"launch.no_eligible_candidate",
			"config.composition_unresolved",
		},
		Fixtures: []string{
			"fixtures/duo-external-v1/session-launch.json",
			"fixtures/duo-external-v1/session-launch-exhausted.json",
		},
		Milestone: true,
	},
	{
		Name:           "session.list",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"session.metadata.read"},
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"session", "list"},
		MCPTool:        "duo_session_list",
		Route:          &Route{Method: "GET", Path: "/v1/sessions"},
		Fixtures:       []string{"fixtures/duo-external-v1/session-list.json"},
		Milestone:      true,
	},
	{
		Name:           "session.inspect",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"session.metadata.read"},
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"session", "show"},
		MCPTool:        "duo_session_inspect",
		Route:          &Route{Method: "GET", Path: "/v1/sessions/{session_id}"},
		Fixtures:       []string{"fixtures/duo-external-v1/session-inspect.json"},
		Milestone:      true,
	},
	{
		Name:           "session.enroll",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"session.manage"},
		Idempotency:    IdempotencyRequired,
		Audit:          AuditPrivilegedWrite,
		CLI:            []string{"session", "enroll"},
		MCPTool:        "",
		Route:          nil,
		// Enrollment fixtures are authored before the Stage 1 gate runs
		// (G-13); the row carries none until then.
		Milestone: true,
	},
	{
		Name:           "session.detach",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"session.manage"},
		Idempotency:    IdempotencyRequired,
		Audit:          AuditPrivilegedWrite,
		CLI:            []string{"session", "detach"},
		MCPTool:        "",
		Route:          nil,
		Milestone:      true,
	},
	{
		Name:           "session.reattach",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"session.manage"},
		Idempotency:    IdempotencyRequired,
		Audit:          AuditPrivilegedWrite,
		CLI:            []string{"session", "reattach"},
		MCPTool:        "",
		Route:          nil,
		Milestone:      true,
	},
	{
		// The planning set names the command (`duo manifest`) but not its
		// semantic operation name; "manifest.show" is authored here — see
		// docs/registry/decisions.md.
		Name:           "manifest.show",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   manifestV1,
		Permissions:    nil, // installed-product read; no grant required
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"manifest"},
		MCPTool:        "",
		Route:          nil,
		Fixtures:       []string{"fixtures/duo-external-v1/manifest.json"},
		Milestone:      true,
	},
	{
		// As with manifest.show, "doctor.run" is an authored operation
		// name for the accepted `duo doctor` administration command.
		Name:           "doctor.run",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"diagnostics.read"},
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"doctor"},
		MCPTool:        "",
		Route:          nil,
		Milestone:      true,
	},
	{
		// "config.migrate" is an authored operation name for the
		// duo-vnext-installation-contract.md §1.3 `duo config migrate`
		// verb (workplan Step 07). It is not a dogfood-milestone
		// operation, so Milestone stays the zero value (false).
		//
		// Permissions: no name in the registered permission vocabulary
		// (duo-vnext-access-errors-audit.md §1.1) covers "author or
		// transform a local configuration document" — config.migrate
		// never opens the authority store or reaches a live daemon, the
		// same reasoning manifest.show's row states ("installed-product
		// read; no grant required"), extended here to a local,
		// operator-invoked read/write of a document the operator names on
		// the command line. Flagged per docs/registry/decisions.md
		// ("Permission gaps in the planning set, flagged").
		Name:           "config.migrate",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    nil,
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"config", "migrate"},
		MCPTool:        "",
		Route:          nil,
	},

	// --- duo.config/v3 workspace<->host correlation verbs ---
	//
	// Added 2026-08-24 (handoff 22, notes/43 item 13). v3 stops authoring
	// the session host in configuration, so the host instance a workspace
	// spawns into is state: `duo workspace host show` reads it and
	// `duo workspace host rebind` changes it under audit. Both are
	// local_admin and therefore absent from the initial generated MCP tool
	// set — a correlation is installation state, and an in-session agent
	// that could repoint where the next spawn lands is the same
	// self-activation hazard that keeps session.launch out.
	{
		// "workspace.host.show" is an authored operation name for the
		// accepted `duo workspace host show` command, on the same footing
		// as manifest.show and doctor.run (docs/registry/decisions.md).
		Name:           "workspace.host.show",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"workspace.read"},
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"workspace", "host", "show"},
		MCPTool:        "",
		Route:          nil,
		// The v3 launch fixtures are re-authored in the same stage; this
		// verb's own envelope has no synced fixture yet.
		Milestone: true,
	},
	{
		Name:           "workspace.host.rebind",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"workspace.mutate"},
		// Required, not optional: the write is durable and attributable,
		// and a retried rebind must not silently append a second audited
		// change of the same correlation.
		Idempotency: IdempotencyRequired,
		Audit:       AuditPrivilegedWrite,
		CLI:         []string{"workspace", "host", "rebind"},
		MCPTool:     "",
		Route:       nil,
		Milestone:   true,
	},

	// --- Contract-attested v1 surface beyond the milestone (data rows) ---
	{
		Name:           "session.remove",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"session.manage"},
		Idempotency:    IdempotencyRequired,
		Audit:          AuditPrivilegedWrite,
		CLI:            []string{"session", "remove"},
		MCPTool:        "", // MCP omits session removal (projection contract §3.1)
		Route:          nil,
	},
	{
		Name:           "conversation.list",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"conversation.read"},
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"conversation", "list"},
		MCPTool:        "duo_conversation_read",
		Route:          &Route{Method: "GET", Path: "/v1/sessions/{session_id}/conversation"},
		Fixtures:       []string{"fixtures/duo-external-v1/conversation-page.json"},
	},
	{
		Name:           "conversation.subscribe",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalDef("stream_item"),
		Permissions:    []string{"conversation.read"},
		Idempotency:    IdempotencyNotApplicable,
		StreamFamily:   "conversation",
		Audit:          AuditNone,
		CLI:            []string{"conversation", "subscribe"},
		MCPTool:        "duo_subscription_open",
		Route:          &Route{Method: "GET", Path: "/v1/streams/conversation"},
		ErrorCodes:     []string{"history.cursor_expired"},
		Fixtures:       []string{"fixtures/duo-external-v1/cursor-expired.json"},
	},
	{
		Name:           "runtime_configuration.subscribe",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalDef("stream_item"),
		// The permission-name table has no separate runtime-configuration
		// read name; session.metadata.read is the closest registered
		// grant. Flagged in docs/registry/decisions.md.
		Permissions:  []string{"session.metadata.read"},
		Idempotency:  IdempotencyNotApplicable,
		StreamFamily: "session.runtime_configuration",
		Audit:        AuditNone,
		CLI:          []string{"session", "runtime-configuration", "subscribe"},
		MCPTool:      "duo_subscription_open",
		Route:        &Route{Method: "GET", Path: "/v1/streams/session.runtime_configuration"},
		ErrorCodes:   []string{"history.cursor_expired"},
		Fixtures: []string{
			"fixtures/duo-external-v1/runtime-configuration-selected.json",
			"fixtures/duo-external-v1/runtime-configuration-effective.json",
		},
	},
	{
		Name:           "prompt.deliver",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"prompt.deliver"},
		Idempotency:    IdempotencyRequired,
		StreamFamily:   "command.results",
		Audit:          AuditSensitiveWrite,
		CLI:            []string{"prompt", "send"},
		MCPTool:        "duo_prompt_deliver",
		Route:          &Route{Method: "POST", Path: "/v1/sessions/{session_id}/prompt-commands"},
		ErrorCodes: []string{
			"prompt.not_ready",
			"prompt.human_priority_hold",
			"command.idempotency_conflict",
			"command.queue_full",
			"command.expired",
			"session.target_exited",
		},
		Fixtures: []string{
			"fixtures/duo-external-v1/prompt-queued.json",
			"fixtures/duo-external-v1/command-result-stream-item.json",
		},
	},
	{
		Name:           "command.inspect",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		// No command-read permission name is registered; flagged in
		// docs/registry/decisions.md.
		Permissions: []string{"session.metadata.read"},
		Idempotency: IdempotencyNotApplicable,
		Audit:       AuditNone,
		CLI:         []string{"prompt", "show"},
		MCPTool:     "duo_command_inspect",
		Route:       &Route{Method: "GET", Path: "/v1/commands/{command_id}"},
		Fixtures:    []string{"fixtures/duo-external-v1/command-inspect.json"},
	},
	{
		Name:           "prompt.lease.acquire",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"prompt.lease"},
		Idempotency:    IdempotencyRequired,
		Audit:          AuditSensitiveWrite,
		CLI:            []string{"prompt", "lease", "acquire"},
		MCPTool:        "duo_lease_manage",
		Route:          &Route{Method: "POST", Path: "/v1/sessions/{session_id}/composer-lease"},
		ErrorCodes: []string{
			"lease.held",
			"lease.unsupported_topology",
		},
		Fixtures: []string{
			"fixtures/duo-external-v1/lease-acquired.json",
			"fixtures/duo-external-v1/lease-refused.json",
		},
	},
	{
		Name:           "terminal.read",
		Projectability: Deterministic,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"terminal.read"},
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"terminal", "read"},
		MCPTool:        "duo_terminal_read",
		Route:          &Route{Method: "GET", Path: "/v1/sessions/{session_id}/terminal/snapshot"},
		Fixtures:       []string{"fixtures/duo-external-v1/terminal-snapshot.json"},
	},

	// --- Provider state (duo-config-v3 step 08) ---
	//
	// `duo provider disable|enable|list` are local_admin, CLI-only: the
	// synced contract set names no provider.* operation and carries no
	// fixture for one yet, so these rows follow session.enroll's shape
	// (externalV1 envelope, no named result def) rather than one of their
	// own. Disabling or enabling a name no launch_variant declares is a
	// normal, non-error result (workplan Step 08), so no operation-specific
	// error code is registered beyond the shared baseline.
	{
		Name:           "provider.disable",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"session.manage"},
		Idempotency:    IdempotencyOptional,
		Audit:          AuditPrivilegedWrite,
		CLI:            []string{"provider", "disable"},
		MCPTool:        "",
		Route:          nil,
	},
	{
		Name:           "provider.enable",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"session.manage"},
		Idempotency:    IdempotencyOptional,
		Audit:          AuditPrivilegedWrite,
		CLI:            []string{"provider", "enable"},
		MCPTool:        "",
		Route:          nil,
	},
	{
		Name:           "provider.list",
		Projectability: LocalAdmin,
		RequestSchema:  externalV1,
		ResultSchema:   externalV1,
		Permissions:    []string{"diagnostics.read"},
		Idempotency:    IdempotencyNotApplicable,
		Audit:          AuditNone,
		CLI:            []string{"provider", "list"},
		MCPTool:        "",
		Route:          nil,
	},
}

// deferredOperations names operations the synced contract set mentions but
// the table deliberately omits, each with the reason. Attestation is
// necessary for a row, not sufficient: a row also needs a determinable
// permission, CLI path, and projection shape, and inventing those would put
// guessed metadata into the manifest. The fixture-coverage test requires
// every operation a synced fixture names to be either registered here or
// listed below, so a newly synced family cannot pass unnoticed.
var deferredOperations = map[string]string{
	"collaboration.value.replace": "attested only by the version-conflict.json error envelope; " +
		"the collaboration families' permission split, CLI paths, and v1 routes are not in the synced set",
}

// stableErrorCodes is the registered stable v1 error-code set, keyed to its
// closed class (duo-vnext-access-errors-audit.md §4.1, verbatim).
var stableErrorCodes = map[string]ErrorClass{
	"invalid.request":                  "invalid",
	"invalid.precondition":             "invalid",
	"invalid.unsupported_schema_value": "invalid",
	"invalid.transport_request":        "invalid",
	"launch.constraints_exhausted":     "invalid",

	"object.not_found":    "not_found",
	"reference.not_found": "not_found",
	"preset.not_found":    "not_found",

	"collaboration.version_mismatch": "conflict",
	"collaboration.path_conflict":    "conflict",
	"command.idempotency_conflict":   "conflict",
	"session.target_exited":          "conflict",
	"prompt.not_ready":               "conflict",
	"prompt.human_priority_hold":     "conflict",
	"history.cursor_expired":         "conflict",
	"lease.held":                     "conflict",
	"lease.not_active":               "conflict",
	"projection.modified":            "conflict",
	"projection.user_file_conflict":  "conflict",

	"operation.unsupported":             "unsupported",
	"operation.realization_unsupported": "unsupported",
	"projection.component_unverified":   "unsupported",
	"lease.unsupported_topology":        "unsupported",

	"operation.temporarily_unavailable": "unavailable",
	"command.queue_full":                "unavailable",
	"config.composition_unresolved":     "unavailable",
	"launch.no_eligible_candidate":      "unavailable",

	"operation.quality_insufficient": "degraded",

	"policy.permission_denied":   "unauthorized",
	"policy.scope_denied":        "unauthorized",
	"policy.enrollment_required": "unauthorized",

	"integration.disconnected": "disconnected",

	"wait.deadline":              "timeout",
	"command.expired":            "timeout",
	"attempt.deadline_no_effect": "timeout",

	"attempt.effect_unknown": "indeterminate",

	"internal.failure": "internal",
}

// permissionNames is the registered permission vocabulary
// (duo-vnext-access-errors-audit.md §1.1, verbatim).
var permissionNames = []string{
	"session.metadata.read",
	"conversation.read",
	"prompt.deliver",
	"prompt.release",
	"prompt.lease",
	"terminal.read",
	"terminal.resize",
	"terminal.input",
	"session.manage",
	"workspace.read",
	"workspace.mutate",
	"collaboration.read",
	"collaboration.mutate",
	"collaboration.lifecycle",
	"collaboration.remove",
	"collaboration.owner.transfer",
	"subscription.manage",
	"agent.activate",
	"notification.requeue",
	"collaboration.acknowledge",
	"audit.read",
	"diagnostics.read",
	"reporter.submit",
	"harness.manage",
}
