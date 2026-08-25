package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/store"
)

// dogfoodExportSQLRel is the CLI-package relative path to the airgapped
// export. Never written; only read for restore into t.TempDir / XDG.
const (
	dogfoodExportSQLRel = "../domain/testdata/duo-db-export.sql"
	dogfoodExportSHA256 = "ac5d786096ed89767a31f514b6758a8a6c05afa08daca0decccc120c134f0aba"
	enrolledStaleID     = "ses_285f7397443f62a619036e3a878e362b"
)

// paneAbsentValidator reports ContinuityPaneAbsent for every claim. Fixture
// tests never dial Herdr.
type paneAbsentValidator struct{}

func (paneAbsentValidator) ValidateAttachment(_ context.Context, _ host.HostAttachmentClaim) (host.HostContinuityEvidence, error) {
	return host.ContinuityEvidence(host.ContinuityPaneAbsent, host.Evidence{}), nil
}

// unreachableValidator fails every probe with ErrUnreachable.
type unreachableValidator struct{}

func (unreachableValidator) ValidateAttachment(_ context.Context, _ host.HostAttachmentClaim) (host.HostContinuityEvidence, error) {
	return host.HostContinuityEvidence{}, host.Unreachable(nil)
}

func installPaneAbsentValidator(t *testing.T) {
	t.Helper()
	orig := reconcileValidatorFor
	t.Cleanup(func() { reconcileValidatorFor = orig })
	reconcileValidatorFor = func(string) (host.HostAttachmentValidator, error) {
		return paneAbsentValidator{}, nil
	}
}

func installUnreachableValidator(t *testing.T) {
	t.Helper()
	orig := reconcileValidatorFor
	t.Cleanup(func() { reconcileValidatorFor = orig })
	reconcileValidatorFor = func(string) (host.HostAttachmentValidator, error) {
		return unreachableValidator{}, nil
	}
}

// dogfoodExportSQLPath resolves the fixture SQL from this test file's
// location so cwd does not matter.
func dogfoodExportSQLPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(thisFile), dogfoodExportSQLRel)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture SQL %s: %v (must not touch the live store)", path, err)
	}
	return path
}

func assertDogfoodExportUnchanged(t *testing.T) {
	t.Helper()
	path := dogfoodExportSQLPath(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture SQL: %v", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hash fixture SQL: %v", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != dogfoodExportSHA256 {
		t.Fatalf("fixture SQL sha256 = %s, want %s (source SQL must not be mutated)", got, dogfoodExportSHA256)
	}
}

// restoreDogfoodExportTo applies the fixture SQL into dbPath via python3
// executescript. The source SQL is never written.
func restoreDogfoodExportTo(t *testing.T, dbPath, sqlPath string) {
	t.Helper()
	cmd := exec.Command("python3", "-c",
		"import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.executescript(open(sys.argv[2]).read()); c.commit()",
		dbPath, sqlPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restoring dogfood export to %s: %v\n%s", dbPath, err, out)
	}
}

// installDogfoodFixtureAtXDG restores the export to
// $XDG_DATA_HOME/duo/duo.db under an isolated TempDir. Never opens
// ~/.local/share/duo/duo.db.
func installDogfoodFixtureAtXDG(t *testing.T) string {
	t.Helper()
	assertDogfoodExportUnchanged(t)

	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	duoDir := filepath.Join(xdg, "duo")
	if err := os.MkdirAll(duoDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", duoDir, err)
	}
	dbPath := filepath.Join(duoDir, "duo.db")
	restoreDogfoodExportTo(t, dbPath, dogfoodExportSQLPath(t))
	return dbPath
}

func openFixtureAuthorityNoCleanup(t *testing.T, dbPath string) (*store.Store, *domain.Authority) {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dbPath, err)
	}
	a, err := domain.Open(context.Background(), storerepo.New(s))
	if err != nil {
		_ = s.Close()
		t.Fatalf("domain.Open: %v", err)
	}
	return s, a
}

func openFixtureAuthority(t *testing.T, dbPath string) (*store.Store, *domain.Authority) {
	t.Helper()
	s, a := openFixtureAuthorityNoCleanup(t, dbPath)
	t.Cleanup(func() { _ = s.Close() })
	return s, a
}

func countFactKind(t *testing.T, s *store.Store, kind string) int {
	t.Helper()
	items, err := s.ReadStream(context.Background(), "duo.domain.fact/v1", 0, 5000)
	if err != nil {
		t.Fatalf("ReadStream: %v", err)
	}
	n := 0
	for _, item := range items {
		var payload struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
			t.Fatalf("unmarshal seq %d: %v", item.Seq, err)
		}
		if payload.Kind == kind {
			n++
		}
	}
	return n
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func writeFixtureReconcileEvidence(t *testing.T, rawJSON string) {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), "evidence", "traces", "recovery")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir evidence: %v", err)
	}
	path := filepath.Join(dir, "fixture-reconcile.json")
	if err := os.WriteFile(path, []byte(rawJSON), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestFixtureReconcilePaneAbsent applies the locked pane-absent path to the
// restored dogfood export: attachments → exited, empty attachment lists →
// replaced (instanceOutcome empty-classes branch; cannot prove pane-absent).
func TestFixtureReconcilePaneAbsent(t *testing.T) {
	dbPath := installDogfoodFixtureAtXDG(t)
	installPaneAbsentValidator(t)

	zeroAttSessions, attSessions := map[domain.SessionID]bool{}, map[domain.SessionID]bool{}
	func() {
		s, before := openFixtureAuthorityNoCleanup(t, dbPath)
		defer func() { _ = s.Close() }()
		if got := len(before.Sessions()); got != 25 {
			t.Fatalf("sessions before = %d, want 25", got)
		}
		if got := before.ActiveClaims(); got != 23 {
			t.Fatalf("ActiveClaims before = %d, want 23", got)
		}
		if got := len(before.Recovering()); got != 25 {
			t.Fatalf("Recovering before = %d, want 25", got)
		}
		for _, ses := range before.Sessions() {
			if len(before.Attachments(ses.ID)) == 0 {
				zeroAttSessions[ses.ID] = true
			} else {
				attSessions[ses.ID] = true
			}
		}
		// Fixture shape: 23 attachments across 22 sessions; 3 sessions have an
		// empty Attachments() list (step brief said 2 — assert the real count).
		if len(zeroAttSessions) != 3 {
			t.Fatalf("zero-attachment sessions = %d, want 3 (empty list → RecoveryReplaced)", len(zeroAttSessions))
		}
		if len(attSessions) != 22 {
			t.Fatalf("sessions with attachments = %d, want 22", len(attSessions))
		}
		enrolled, ok := before.Session(domain.SessionID(enrolledStaleID))
		if !ok {
			t.Fatalf("enrolled session %s missing", enrolledStaleID)
		}
		enrolledInst, ok := before.Instance(enrolled.Current)
		if !ok || enrolledInst.State != domain.InstanceLive {
			t.Fatalf("enrolled instance state = %+v, want live", enrolledInst)
		}
		atts := before.Attachments(enrolled.ID)
		if len(atts) != 1 || atts[0].Container != "w2:p5" {
			t.Fatalf("enrolled attachments = %+v, want one w2:p5", atts)
		}
	}()

	code, out, errOut := runSession(t, "session", "reconcile", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	writeFixtureReconcileEvidence(t, out)
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 25 {
		t.Fatalf("reconcile items = %d, want 25", len(result.Items))
	}

	exited, replaced, other := 0, 0, 0
	for _, it := range result.Items {
		sid := domain.SessionID(it.SessionID)
		switch it.Outcome {
		case string(domain.RecoveryExited):
			exited++
			if zeroAttSessions[sid] {
				t.Errorf("%s: zero attachments mapped to exited; want replaced (empty-classes branch)", sid)
			}
		case string(domain.RecoveryReplaced):
			replaced++
			if !zeroAttSessions[sid] {
				t.Errorf("%s: had attachments but outcome=replaced; want exited under pane-absent", sid)
			}
			if it.Reason != "no attachments to probe" {
				t.Errorf("%s replaced reason = %q, want no attachments to probe", sid, it.Reason)
			}
		default:
			other++
			t.Errorf("%s: outcome = %q, want exited or replaced", sid, it.Outcome)
		}
	}
	if exited != 22 || replaced != 3 || other != 0 {
		t.Fatalf("outcomes exited=%d replaced=%d other=%d, want 22/3/0", exited, replaced, other)
	}

	var recoveryFacts int
	func() {
		s, after := openFixtureAuthorityNoCleanup(t, dbPath)
		defer func() { _ = s.Close() }()
		if got := after.ActiveClaims(); got != 0 {
			t.Fatalf("ActiveClaims after = %d, want 0 (23 held claims released)", got)
		}
		if got := len(after.Recovering()); got != 0 {
			t.Fatalf("Recovering after = %d, want 0", got)
		}
		for _, ses := range after.Sessions() {
			inst, ok := after.Instance(ses.Current)
			if !ok {
				t.Fatalf("session %s current instance missing", ses.ID)
			}
			if inst.State != domain.InstanceExited {
				t.Errorf("session %s instance state = %q, want exited", ses.ID, inst.State)
			}
			view, _ := after.View(ses.ID)
			if view != domain.ViewInactive {
				t.Errorf("session %s view = %q, want inactive", ses.ID, view)
			}
		}
		enrolledAfter, _ := after.Session(domain.SessionID(enrolledStaleID))
		instAfter, _ := after.Instance(enrolledAfter.Current)
		if instAfter.State != domain.InstanceExited {
			t.Errorf("enrolled %s state = %q, want exited", enrolledStaleID, instAfter.State)
		}
		recoveryFacts = countFactKind(t, s, "instance.recovery-decision")
		if recoveryFacts != 25 {
			t.Fatalf("instance.recovery-decision after first reconcile = %d, want 25", recoveryFacts)
		}
		stateFacts := countFactKind(t, s, "instance.state")
		if stateFacts < 25 {
			t.Fatalf("instance.state after pane-absent = %d, want >= 25 (exits)", stateFacts)
		}
	}()

	// Repeat reconcile: terminal instances leave Recovering(); fact count
	// must not increase.
	code, out, errOut = runSession(t, "session", "reconcile", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("second reconcile: exit %d (stderr: %s)", code, errOut)
	}
	again := parseReconcileJSON(t, out)
	if len(again.Items) != 0 {
		t.Fatalf("second reconcile items = %d, want 0", len(again.Items))
	}
	func() {
		s2, after2 := openFixtureAuthorityNoCleanup(t, dbPath)
		defer func() { _ = s2.Close() }()
		if got := countFactKind(t, s2, "instance.recovery-decision"); got != recoveryFacts {
			t.Fatalf("recovery-decision after repeat = %d, want unchanged %d", got, recoveryFacts)
		}
		if got := len(after2.Recovering()); got != 0 {
			t.Fatalf("Recovering after repeat = %d, want 0", got)
		}
	}()

	// list / show: view inactive, lifecycle wire stays active (archive is step 10).
	code, out, errOut = runSession(t, "session", "list", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("list: exit %d (stderr: %s)", code, errOut)
	}
	var listEnv struct {
		Result sessionListResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &listEnv); err != nil {
		t.Fatalf("list JSON: %v", err)
	}
	if len(listEnv.Result.Items) != 25 {
		t.Fatalf("list items = %d, want 25 (removed-filter is step 10)", len(listEnv.Result.Items))
	}
	for _, it := range listEnv.Result.Items {
		if it.View != string(domain.ViewInactive) {
			t.Errorf("list %s view = %q, want inactive", it.SessionID, it.View)
		}
		if it.Lifecycle != "active" {
			t.Errorf("list %s lifecycle = %q, want active", it.SessionID, it.Lifecycle)
		}
	}

	view, lifecycle := showView(t, enrolledStaleID)
	if view != string(domain.ViewInactive) {
		t.Errorf("show enrolled view = %q, want inactive", view)
	}
	if lifecycle != "active" {
		t.Errorf("show enrolled lifecycle = %q, want active", lifecycle)
	}
	code, out, errOut = runSession(t, "session", "show", enrolledStaleID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show enrolled: exit %d (stderr: %s)", code, errOut)
	}
	var showEnv struct {
		Result sessionInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &showEnv); err != nil {
		t.Fatalf("show JSON: %v", err)
	}
	if showEnv.Result.InstanceState != string(domain.InstanceExited) {
		t.Errorf("show enrolled instance_state = %q, want exited", showEnv.Result.InstanceState)
	}

	assertDogfoodExportUnchanged(t)
}

// TestFixtureReconcileUnreachableLeavesStartingLive restores a second copy
// and proves unreachable evidence does not exit instances that have
// attachments. Sessions with an empty Attachments() list never call the
// validator — instanceOutcome's empty-classes branch maps them to replaced
// (and exit), which is the same honest mapping as the pane-absent path.
func TestFixtureReconcileUnreachableLeavesStartingLive(t *testing.T) {
	assertDogfoodExportUnchanged(t)
	dbPath := installDogfoodFixtureAtXDG(t)
	installUnreachableValidator(t)

	zeroAttSessions := map[domain.SessionID]bool{}
	var stateBefore, recoveryBefore, claimsBefore int
	starting, live := 0, 0
	func() {
		s0, before := openFixtureAuthorityNoCleanup(t, dbPath)
		defer func() { _ = s0.Close() }()
		stateBefore = countFactKind(t, s0, "instance.state")
		recoveryBefore = countFactKind(t, s0, "instance.recovery-decision")
		claimsBefore = before.ActiveClaims()
		if claimsBefore != 23 {
			t.Fatalf("ActiveClaims before = %d, want 23", claimsBefore)
		}
		for _, ses := range before.Sessions() {
			if len(before.Attachments(ses.ID)) == 0 {
				zeroAttSessions[ses.ID] = true
			}
			inst, _ := before.Instance(ses.Current)
			switch inst.State {
			case domain.InstanceStarting:
				starting++
			case domain.InstanceLive:
				live++
			default:
				t.Fatalf("unexpected state %q on %s", inst.State, ses.ID)
			}
		}
		if starting != 24 || live != 1 {
			t.Fatalf("before unreachable: starting=%d live=%d, want 24/1", starting, live)
		}
		if len(zeroAttSessions) != 3 {
			t.Fatalf("zero-attachment sessions = %d, want 3", len(zeroAttSessions))
		}
	}()

	code, out, errOut := runSession(t, "session", "reconcile", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 25 {
		t.Fatalf("items = %d, want 25", len(result.Items))
	}
	unreachable, replaced, other := 0, 0, 0
	for _, it := range result.Items {
		sid := domain.SessionID(it.SessionID)
		switch it.Outcome {
		case string(domain.RecoveryUnreachable):
			unreachable++
			if zeroAttSessions[sid] {
				t.Errorf("%s: zero attachments mapped to unreachable; empty list never probes", sid)
			}
		case string(domain.RecoveryReplaced):
			replaced++
			if !zeroAttSessions[sid] {
				t.Errorf("%s: had attachments but outcome=replaced under unreachable validator", sid)
			}
		default:
			other++
			t.Errorf("%s: outcome = %q, want unreachable or replaced", sid, it.Outcome)
		}
	}
	if unreachable != 22 || replaced != 3 || other != 0 {
		t.Fatalf("outcomes unreachable=%d replaced=%d other=%d, want 22/3/0", unreachable, replaced, other)
	}

	s1, after := openFixtureAuthority(t, dbPath)
	if got := after.ActiveClaims(); got != claimsBefore {
		t.Fatalf("ActiveClaims after unreachable = %d, want unchanged %d", got, claimsBefore)
	}
	// Only attachment-bearing instances stay recovering; empty-list replaced exits.
	if got := len(after.Recovering()); got != 22 {
		t.Fatalf("Recovering after unreachable = %d, want 22", got)
	}
	stillStarting, stillLive, exited := 0, 0, 0
	for _, ses := range after.Sessions() {
		inst, _ := after.Instance(ses.Current)
		switch {
		case zeroAttSessions[ses.ID]:
			if inst.State != domain.InstanceExited {
				t.Errorf("zero-att %s state = %q, want exited (replaced)", ses.ID, inst.State)
			}
			exited++
		case inst.State == domain.InstanceStarting:
			stillStarting++
			view, _ := after.View(ses.ID)
			if view != domain.ViewRecovering {
				t.Errorf("session %s view = %q, want recovering", ses.ID, view)
			}
		case inst.State == domain.InstanceLive:
			stillLive++
			view, _ := after.View(ses.ID)
			if view != domain.ViewRecovering {
				t.Errorf("session %s view = %q, want recovering", ses.ID, view)
			}
		default:
			t.Errorf("session %s state = %q, unexpected", ses.ID, inst.State)
		}
	}
	if stillStarting != 21 || stillLive != 1 || exited != 3 {
		t.Fatalf("after unreachable: starting=%d live=%d exited=%d, want 21/1/3", stillStarting, stillLive, exited)
	}
	// instance.state only grows for the 3 empty-list replaced exits — not for
	// attachment-bearing unreachable probes.
	if got := countFactKind(t, s1, "instance.state"); got != stateBefore+3 {
		t.Fatalf("instance.state after unreachable = %d, want %d (only empty-list exits)", got, stateBefore+3)
	}
	if got := countFactKind(t, s1, "instance.recovery-decision"); got != recoveryBefore+25 {
		t.Fatalf("recovery-decision = %d, want %d", got, recoveryBefore+25)
	}

	assertDogfoodExportUnchanged(t)
}

// TestReconcileUnreachableAddsNoInstanceStateFacts closes the fake-host gap:
// Disconnect must not write instance.state.
func TestReconcileUnreachableAddsNoInstanceStateFacts(t *testing.T) {
	h, hosts, sessionID, _ := launchRecovering(t)
	dbPath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "duo", "duo.db")
	hosts.hosts[hosts.seen[0].IntegrationInstanceID].Disconnect()
	installReconcileHosts(t, hosts)
	h.close()

	var stateBefore int
	func() {
		s0, _ := openFixtureAuthorityNoCleanup(t, dbPath)
		defer func() { _ = s0.Close() }()
		stateBefore = countFactKind(t, s0, "instance.state")
	}()

	code, out, errOut := runSession(t, "session", "reconcile", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 || result.Items[0].Outcome != string(domain.RecoveryUnreachable) {
		t.Fatalf("result = %+v, want unreachable", result.Items)
	}

	s1, _ := openFixtureAuthority(t, dbPath)
	if got := countFactKind(t, s1, "instance.state"); got != stateBefore {
		t.Fatalf("instance.state = %d, want unchanged %d", got, stateBefore)
	}
}
