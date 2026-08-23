package manifest

// This file holds the three schema-family lists the duo.manifest/v1
// contract's root requires (public_schemas, configuration_schemas,
// projection_formats). Unlike Operations, these have no internal/registry
// view to derive from — none of the three families config/v1, config/v2,
// or projection-stamp/v1 is referenced by any registry.Descriptor — so they
// are authored data here, cross-checked against the embedded contract set
// by TestSchemaFamilyListsResolve rather than derived at runtime.
// docs/config/decisions.md records the "why authored, not derived" call.

// publicSchemas are the wire-truth envelope schemas every public operation's
// requests and results decode under.
var publicSchemas = []string{"duo.external/v1"}

// configurationSchemas are the configuration document schemas this binary
// recognizes. duo.config/v1 documents remain structurally valid per
// duo-vnext-installation-contract.md §1 even though Stage 0's resolver
// (internal/config.ParseV2) only ever loads v2 — recognizing the family is
// a schema-support declaration, not a claim that every operation on it is
// implemented yet.
var configurationSchemas = []string{"duo.config/v1", "duo.config/v2"}

// projectionFormats are the generated-artifact stamp/conformance schemas
// this binary emits or reads.
var projectionFormats = []string{"duo.projection-stamp/v1"}

// schemaFamilyFiles maps every wire schema family string this package
// might declare (WireSchema plus the three lists above) to its backing file
// in contracts.FS, so a test can prove none of them names a family the
// embedded contract set does not actually carry.
var schemaFamilyFiles = map[string]string{
	WireSchema:                      "schemas/duo-manifest-v1.schema.json",
	"duo.external/v1":               "schemas/duo-external-v1.schema.json",
	"duo.config/v1":                 "schemas/duo-config-v1.schema.json",
	"duo.config/v2":                 "schemas/duo-config-v2.schema.json",
	"duo.projection-stamp/v1":       "schemas/duo-projection-stamp-v1.schema.json",
	"duo.projection-conformance/v1": "schemas/duo-projection-conformance-v1.schema.json",
}
