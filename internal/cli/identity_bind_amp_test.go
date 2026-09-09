package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/launch"
	runtimeamp "github.com/procrastivity/duo/internal/runtime/amp"
)

// --- mint-exit test doubles (Amp) -------------------------------------------

// ampPrintMintHosts wraps identityHosts for a print-mint Amp launch test
// double: Start seeds the mint log the materialized wrapper script would
// have tee'd (when mintLog is non-empty) and then kills the pane
// synchronously — the same way the wrapper script exits right after `amp
// -x` mints a thread and closes, before bindLaunchIdentities' first
// AgentOnPane poll ever runs. Mirrors devinPrintMintHosts
// (identity_bind_test.go) exactly, swapping the ATIF export for the Amp
// mint log.
type ampPrintMintHosts struct {
	inner *identityHosts
	// mintLog is the raw JSONL body seeded at amp.MintLogPath. Empty
	// seeds nothing, mirroring devinPrintMintHosts.seedATIF == false.
	mintLog string
}

func (h *ampPrintMintHosts) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	inner, err := h.inner.LauncherFor(t)
	if err != nil {
		return nil, err
	}
	return &ampPrintMintLauncher{Host: inner.(*hostfake.Host), hosts: h}, nil
}

type ampPrintMintLauncher struct {
	*hostfake.Host
	hosts *ampPrintMintHosts
}

func (h *ampPrintMintLauncher) Start(ctx context.Context, prepared host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	ev, err := h.Host.Start(ctx, prepared)
	if err != nil {
		return ev, err
	}
	if h.hosts.mintLog != "" {
		tuple, _ := prepared.Opaque.(host.ResolvedLaunchTuple)
		path, perr := runtimeamp.MintLogPath(prepared.LaunchResolutionID, tuple.Leaf)
		if perr != nil {
			return host.HostLaunchEvidence{}, perr
		}
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			return host.HostLaunchEvidence{}, mkErr
		}
		if wErr := os.WriteFile(path, []byte(h.hosts.mintLog), 0o644); wErr != nil {
			return host.HostLaunchEvidence{}, wErr
		}
	}
	h.Kill(host.Attachment{
		IntegrationInstanceID: ev.Evidence.IntegrationInstanceID,
		PaneID:                ev.Evidence.PaneID,
		HostContainerID:       ev.Evidence.HostContainerID,
	})
	return ev, nil
}

// ampMintLogLine renders one line of Amp's tee'd stream-JSON mint log.
func ampMintLogLine(threadID string) string {
	return `{"type":"system","subtype":"init","session_id":"` + threadID + `"}` + "\n"
}

// ampMintLogSuccess renders a complete mint log: the session_id line plus
// the closing "result"/"success" record ThreadIDFromMintLog requires
// before it will trust the thread id.
func ampMintLogSuccess(threadID string) string {
	return ampMintLogLine(threadID) +
		`{"type":"result","subtype":"success","session_id":"` + threadID + `","result":"DUO-AMP-READY"}` + "\n"
}

// --- mint-exit scenarios (Amp) ----------------------------------------------

// TestPrintMintExitAmpPaneAbsentRecoversIdentity is scenario (a): the
// print-mint process exits (pane_absent) before AgentOnPane ever reports
// identity, but it left a complete mint log behind. bindStartingIdentity
// recovers the minted thread id from that log, binds and marks the
// instance live exactly as an on-time identity report would have, and
// releases the launch fingerprint claim the exited mint process held.
// Mirrors TestPrintMintExitDevinPaneAbsentRecoversIdentity.
func TestPrintMintExitAmpPaneAbsentRecoversIdentity(t *testing.T) {
	setMintExitTiming(t, 200*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond)

	h := newAmpBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	const mintedID = "T-brave-muskmelon"
	hosts := &ampPrintMintHosts{
		inner:   newIdentityHosts(nil),
		mintLog: ampMintLogSuccess(mintedID),
	}
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live after Amp mint-exit recovery", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok || bindings.ExternalAgentSessionID != mintedID {
		t.Fatalf("want agent-session bound to minted id %q, got ok=%v %+v", mintedID, ok, bindings)
	}
	if bindings.TranscriptID != "" {
		t.Errorf("transcript = %q, want empty: Amp keeps no local transcript document", bindings.TranscriptID)
	}
	if heldClaim(t, h, sess) {
		t.Error("fingerprint claim still held after mint-exit release")
	}
}

// TestPrintMintExitAmpNoMintLogTakesGenericExitLeg is scenario (b): the
// same pane_absent exit, but the mint process never wrote a mint log (or
// it never landed). No thread id can be recovered, so the generic exit
// leg runs: Authority.Exit releases every claim through exitInstance, and
// the launch path's loud stderr note fires (the command still succeeds).
// Mirrors TestPrintMintExitDevinNoATIFTakesGenericExitLeg.
func TestPrintMintExitAmpNoMintLogTakesGenericExitLeg(t *testing.T) {
	setMintExitTiming(t, 200*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond)

	h := newAmpBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	hosts := &ampPrintMintHosts{inner: newIdentityHosts(nil)}
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceExited {
		t.Fatalf("instance state = %s, want exited when no mint-log id could be recovered", inst.State)
	}
	if heldClaim(t, h, sess) {
		t.Error("fingerprint claim still held after Authority.Exit")
	}
	if !strings.Contains(h.err.String(), "mint process exit observed") {
		t.Errorf("no loud stderr note about the mint-process exit:\n%s", h.err.String())
	}
}

// TestPrintMintExitAmpIncompleteMintTakesGenericExitLeg is scenario (c): the
// mint log names a thread (session_id) but never closed with a "result"
// / "success" line — amp.ThreadIDFromMintLog reports ErrMintIncomplete,
// which must not recover: the mint never visibly finished, so the generic
// exit leg runs exactly as the no-mint-log case does.
func TestPrintMintExitAmpIncompleteMintTakesGenericExitLeg(t *testing.T) {
	setMintExitTiming(t, 200*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond)

	h := newAmpBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	hosts := &ampPrintMintHosts{
		inner:   newIdentityHosts(nil),
		mintLog: ampMintLogLine("T-incomplete"),
	}
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceExited {
		t.Fatalf("instance state = %s, want exited when the mint log never closed with a success result", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if ok {
		t.Fatalf("want no agent-session bound for an incomplete mint, got %+v", bindings)
	}
	if heldClaim(t, h, sess) {
		t.Error("fingerprint claim still held after Authority.Exit")
	}
	if !strings.Contains(h.err.String(), "mint process exit observed") {
		t.Errorf("no loud stderr note about the mint-process exit:\n%s", h.err.String())
	}
}
