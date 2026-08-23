package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// The Duo-issued identifier types. They are distinct Go types rather than
// bare strings so that a pane ID, a PID, or an agent-runtime session ID
// cannot be assigned into one by accident — decision-01 §3.6's rule that no
// external value is ever a primary Duo handle, enforced by the compiler.
type (
	// WorkspaceID names a durable Duo-owned local work context.
	WorkspaceID string
	// SessionID names a Duo session, the primary public runtime handle.
	SessionID string
	// InstanceID names one runtime instance: one execution generation.
	InstanceID string
	// ActorID names a durable agent actor.
	ActorID string
	// AttachmentID names one session-to-host-container relationship.
	AttachmentID string
	// CorrelationID names one time-bounded external-identifier claim.
	CorrelationID string
	// FactID names one lifecycle fact. It is also the fact log's dedup key.
	FactID string
	// ParkedReportID names one report held as unresolved evidence.
	ParkedReportID string
	// LaunchResolutionID names one launch-resolution record. It is the one
	// Duo ID in this list the kernel does not mint: launch resolution
	// happens before the kernel is called, and the resolver authors the ID
	// so the record can be referenced by the thing that produced it. The
	// kernel stores it and compares it, and parses it no more than it parses
	// any other ID.
	LaunchResolutionID string
)

// ID prefixes. They are diagnostic sugar only: nothing parses an ID, and a
// caller must treat every one of them as opaque.
const (
	workspacePrefix   = "ws"
	sessionPrefix     = "ses"
	instancePrefix    = "ri"
	actorPrefix       = "actor"
	attachmentPrefix  = "att"
	correlationPrefix = "corr"
	factPrefix        = "fact"
	parkedPrefix      = "park"
	credentialPrefix  = "duocred"
)

// mintID returns a fresh opaque ID with the given prefix. 128 random bits:
// IDs are never reused (decision-01 §3.6), so they are drawn from a space
// large enough that "never reused" holds without a counter that a restored
// or copied store could rewind.
func mintID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("domain: minting %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

// mintCredential returns one instance-scoped reporter enrollment credential
// (decision-01 §3.6: secret, never listed as identity) together with the
// fingerprint that is safe to persist. Only the fingerprint reaches the fact
// log; the secret is returned to the caller once and never stored.
func mintCredential() (secret, fingerprint string, err error) {
	secret, err = mintID(credentialPrefix)
	if err != nil {
		return "", "", err
	}
	return secret, credentialFingerprint(secret), nil
}

// credentialFingerprint hashes a presented credential for comparison against
// the stored value.
func credentialFingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
