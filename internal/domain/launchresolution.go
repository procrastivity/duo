package domain

// LaunchResolution is one launch-resolution record as the kernel carries it:
// the record's Duo-authored ID and its serialized body, exactly as the
// component that resolved the launch produced it.
//
// **The body is opaque to the kernel.** The domain stores the bytes it was
// handed and returns the same bytes; it never decodes, re-encodes, validates,
// or interprets them. That is what keeps the §6.9 record — every candidate,
// every elimination reason, the consulted digests, the draw — durable in the
// launch-resolution transaction without teaching the kernel the record's
// schema, which belongs to internal/launch. Re-encoding would also break the
// one property this fact exists to provide: the bytes that come back after a
// restart are the bytes that were committed.
//
// The record is immutable (§6.9: "immutable, Duo-authored evidence"). There
// is no verb that alters one, replay retains the first body recorded for an
// ID, and every accessor returns a copy of the body rather than the stored
// slice.
type LaunchResolution struct {
	// ID is the record's Duo-authored ID. The kernel does not mint it and
	// never parses it; see LaunchResolutionID.
	ID LaunchResolutionID
	// Body is the serialized record. Opaque; see the type comment.
	Body []byte
}

// clone deep-copies a record, body included. A shallow copy would hand a
// caller the stored backing array, and a caller that wrote to it would have
// altered immutable evidence the fact log says is unchanged.
func (r LaunchResolution) clone() LaunchResolution {
	out := LaunchResolution{ID: r.ID}
	if r.Body != nil {
		out.Body = make([]byte, len(r.Body))
		copy(out.Body, r.Body)
	}
	return out
}

// recordedLaunchResolution is one record plus the identities the same
// transaction created for it — §6.9's "eventual session/runtime-instance
// links", held here so the index can answer both directions.
type recordedLaunchResolution struct {
	record   LaunchResolution
	session  SessionID
	instance InstanceID
}

// recordLaunchResolution folds one launch.resolved fact into the index.
//
// First write wins, in replay and at commit alike: the record is immutable,
// so a second fact naming an ID the log already carries changes nothing. It
// cannot happen through Launch (record IDs are never reused), and if it ever
// did, the earlier evidence is the one the spawn was gated on.
func (a *Authority) recordLaunchResolution(rec LaunchResolution, session SessionID, instance InstanceID) {
	if rec.ID == "" {
		return
	}
	if _, ok := a.launchResolutions[rec.ID]; ok {
		return
	}
	entry := &recordedLaunchResolution{record: rec.clone(), session: session, instance: instance}
	a.launchResolutions[rec.ID] = entry
	if session != "" {
		if _, ok := a.sessionLaunch[session]; !ok {
			a.sessionLaunch[session] = rec.ID
		}
	}
}

// LaunchResolution returns one recorded launch-resolution record, body and
// all. The body is a copy.
func (a *Authority) LaunchResolution(id LaunchResolutionID) (LaunchResolution, bool) {
	entry, ok := a.launchResolutions[id]
	if !ok {
		return LaunchResolution{}, false
	}
	return entry.record.clone(), true
}

// LaunchResolutionBinding returns the session and runtime instance minted
// in the same transaction as the launch-resolution record. ok is false when
// no such record was committed — a refused launch, or an ID that never
// reached the kernel.
//
// The instance here is the one the record created, not Session.Current: a
// later restart mints a new instance that the original harness directory
// does not belong to. Doctor's harness sweep keys on this binding.
func (a *Authority) LaunchResolutionBinding(id LaunchResolutionID) (session SessionID, instance InstanceID, ok bool) {
	entry, ok := a.launchResolutions[id]
	if !ok {
		return "", "", false
	}
	return entry.session, entry.instance, true
}

// SessionLaunchResolution returns the launch-resolution record that explains
// one session, when the launch that created it committed one. An enrolled
// session has none: nothing resolved it.
func (a *Authority) SessionLaunchResolution(id SessionID) (LaunchResolution, bool) {
	recordID, ok := a.sessionLaunch[id]
	if !ok {
		return LaunchResolution{}, false
	}
	return a.LaunchResolution(recordID)
}
