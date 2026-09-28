package devclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/store"
)

// The fixture pin is intentionally independent of the subject's reported
// build. Changing the separate checkout requires an explicit requalification.
const fixtureBuild = "sha256:2971734021e91eabae721cffa818530814f31e68be81d4aee4e852bd9967cee3"

type subjectDocument struct {
	Schema string          `json:"schema"`
	Kind   string          `json:"kind"`
	Value  json.RawMessage `json:"value"`
}

func document(t *testing.T, result json.RawMessage, kind string, value any) {
	t.Helper()
	var doc subjectDocument
	if err := json.Unmarshal(result, &doc); err != nil || doc.Schema != "agent.harness/v0" || doc.Kind != kind {
		t.Fatalf("subject document: schema=%q kind=%q error=%v, want %s", doc.Schema, doc.Kind, err, kind)
	}
	if err := json.Unmarshal(doc.Value, value); err != nil {
		t.Fatalf("decode %s value: %v", kind, err)
	}
}

func call(t *testing.T, c Client, kind, operation string, fields map[string]any, value any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := c.Call(ctx, operation, fields)
	if err != nil {
		t.Fatalf("%s: %v", operation, err)
	}
	document(t, result, kind, value)
}

func pinFixture(t *testing.T, program string) {
	t.Helper()
	sum := sha256.New()
	for i, name := range []string{"own_agent.py", "deterministic_worker.py", "opencode_worker.py", "human_console.py"} {
		if i > 0 {
			_, _ = sum.Write([]byte{0})
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(program), name))
		if err != nil {
			t.Fatalf("read pinned subject source: %v", err)
		}
		_, _ = sum.Write(data)
	}
	if got := "sha256:" + hex.EncodeToString(sum.Sum(nil)); got != fixtureBuild {
		t.Fatalf("subject source changed: build %s, want %s; requalify before updating the pin", got, fixtureBuild)
	}
}

func startSubject(t *testing.T, program, root string) *exec.Cmd {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("locate Python for external subject: %v", err)
	}
	cmd := exec.Command(python, "-I", "-B", program, "--state-dir", root)
	cmd.Env = []string{"PYTHONDONTWRITEBYTECODE=1"}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start external subject: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState != nil { // The test may have stopped this owner to exercise restart.
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("external subject shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("external subject did not stop after SIGTERM")
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "agent.sock")); err == nil {
			return cmd
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("external subject did not create its socket")
	return nil
}

func TestPrivateProtocolOwnedConsumer(t *testing.T) {
	program := os.Getenv("DUO_AGENT_PROGRAM")
	if program == "" {
		t.Skip("requires a separate, pinned duo-agent checkout: set DUO_AGENT_PROGRAM to its absolute own_agent.py path")
	}
	if !filepath.IsAbs(program) {
		t.Fatal("DUO_AGENT_PROGRAM must be absolute")
	}
	pinFixture(t, program)
	root := filepath.Join(t.TempDir(), "owner")
	ctx := context.Background()

	// Duo mints its own hostless identity before the external subject starts.
	duoStore, err := store.OpenAuthority(filepath.Join(t.TempDir(), "duo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = duoStore.Close() })
	authority, err := domain.Open(ctx, storerepo.New(duoStore))
	if err != nil {
		t.Fatal(err)
	}
	launched, err := authority.Launch(ctx, domain.LaunchRequest{RootPath: t.TempDir(), Actor: "dev-test"})
	if err != nil {
		t.Fatal(err)
	}
	child := startSubject(t, program, root)
	if child.Process.Pid == os.Getpid() {
		t.Fatal("subject must be a distinct process")
	}
	token, err := os.ReadFile(filepath.Join(root, "token"))
	if err != nil {
		t.Fatal(err)
	}
	client := Client{Socket: filepath.Join(root, "agent.sock"), Token: string(token)}
	var described struct {
		OwnerID        string   `json:"owner_id"`
		Profiles       []string `json:"profiles"`
		Implementation struct {
			Build string `json:"build"`
		} `json:"implementation"`
	}
	call(t, client, "describe", "protocol.describe", nil, &described)
	if described.OwnerID == "" || described.Implementation.Build != fixtureBuild || len(described.Profiles) != 0 {
		t.Fatalf("describe: expected pinned build and no profile claim; owner=%t build=%q profiles=%v",
			described.OwnerID != "", described.Implementation.Build, described.Profiles)
	}
	deadline := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second).Format("2006-01-02T15:04:05Z")
	create := func(label string) map[string]any {
		return map[string]any{"write": map[string]any{
			"operation": "session.create", "target": map[string]string{"owner_id": described.OwnerID},
			"idempotency_key": "duo_dev_create", "deadline": deadline, "payload": map[string]string{"label": label},
			"preconditions": map[string]any{}, "grant": "local",
		}}
	}
	var created struct {
		CommandID string `json:"command_id"`
		State     string `json:"state"`
		Result    struct {
			SessionID string `json:"session_id"`
		} `json:"result"`
	}
	call(t, client, "command", "session.create", create("Duo dev"), &created)
	if created.State != "completed" || created.Result.SessionID == "" || created.Result.SessionID == string(launched.Session) {
		t.Fatalf("create: result not a distinct completed owner session: %+v", created)
	}
	var repeated struct {
		CommandID string `json:"command_id"`
	}
	call(t, client, "command", "session.create", create("Duo dev"), &repeated)
	if repeated.CommandID != created.CommandID {
		t.Fatal("identical create changed its command ID")
	}
	var conflict struct {
		Class  string `json:"class"`
		Effect string `json:"effect"`
	}
	call(t, client, "refusal", "session.create", create("Changed"), &conflict)
	if conflict.Class != "conflict" || conflict.Effect != "no_effect" {
		t.Fatalf("changed create did not refuse without effect: %+v", conflict)
	}
	target := map[string]string{"owner_id": described.OwnerID, "session_id": created.Result.SessionID}
	var observed struct {
		OwnerID       string `json:"owner_id"`
		SessionID     string `json:"session_id"`
		IncarnationID string `json:"incarnation_id"`
		Revision      string `json:"revision"`
	}
	call(t, client, "session", "session.inspect", map[string]any{"target": target}, &observed)
	if observed.OwnerID != described.OwnerID || observed.SessionID != created.Result.SessionID || observed.IncarnationID == "" {
		t.Fatalf("inspection crossed owner/session scope: %+v", observed)
	}
	var supported struct {
		Operation    string `json:"operation"`
		Availability string `json:"availability"`
		Pin          struct {
			Build string `json:"implementation_build"`
		} `json:"pin"`
	}
	call(t, client, "support", "support.inspect", map[string]any{
		"target": target, "operation_name": "conversation.snapshot",
	}, &supported)
	if supported.Operation != "conversation.snapshot" || supported.Availability != "available" || supported.Pin.Build != fixtureBuild {
		t.Fatalf("snapshot support is not pinned and scoped: %+v", supported)
	}

	// This is an explicit development-owner association, not an instance
	// credential claim from the subject and not a production integration.
	scope := "agent.harness/v0@" + described.OwnerID
	if err := authority.Bind(ctx, domain.BindRequest{
		Session: launched.Session, Actor: "dev-test",
		Attestation:  domain.Attestation{Source: domain.SourceOwner, Subject: "dev-test"},
		AgentSession: domain.AgentSessionRef{IntegrationInstance: scope, SessionID: observed.SessionID},
		Reason:       "authenticated private owner observation",
	}); err != nil {
		t.Fatalf("hostless association: %v", err)
	}
	if err := authority.MarkLive(ctx, launched.Instance, "dev-test", "independent owner process and session response"); err != nil {
		t.Fatal(err)
	}
	duoSession, ok := authority.Session(launched.Session)
	if !ok || duoSession.Current != launched.Instance || duoSession.Attachment != "" || len(authority.Attachments(launched.Session)) != 0 {
		t.Fatalf("hostless Duo identity changed unexpectedly: %+v", duoSession)
	}
	correlations := authority.Correlations(domain.TargetInstance, string(launched.Instance))
	if len(correlations) != 1 || correlations[0].ExternalKind != "agent.session" ||
		correlations[0].ExternalValue != observed.SessionID || correlations[0].Scope != scope {
		t.Fatalf("Duo association did not preserve scoped owner session identity: %+v", correlations)
	}

	var first struct {
		Barrier map[string]string `json:"barrier"`
		Items   []json.RawMessage `json:"items"`
	}
	call(t, client, "snapshot", "conversation.snapshot", map[string]any{
		"target": target, "page_size": 1,
	}, &first)
	if len(first.Items) != 0 || first.Barrier["position"] != "1" || first.Barrier["session_id"] != observed.SessionID {
		t.Fatalf("initial snapshot does not name the empty conversation and create barrier: %+v", first)
	}
	turnWrite := map[string]any{
		"operation": "turn.submit", "target": map[string]string{
			"owner_id": described.OwnerID, "session_id": observed.SessionID, "incarnation_id": observed.IncarnationID,
		}, "idempotency_key": "duo_dev_turn", "deadline": deadline, "queue_policy": "require_ready",
		"payload":       map[string]any{"blocks": []any{map[string]string{"type": "text", "content": "Alpha 17"}}},
		"preconditions": map[string]string{"session_revision": observed.Revision}, "grant": "local",
	}
	var delivered struct {
		CommandID string `json:"command_id"`
		State     string `json:"state"`
		Result    struct {
			TurnID string `json:"turn_id"`
		} `json:"result"`
	}
	call(t, client, "command", "turn.submit", map[string]any{"write": turnWrite}, &delivered)
	if delivered.State != "completed" || delivered.Result.TurnID == "" {
		entries, _ := os.ReadDir(filepath.Join(root, "effects"))
		t.Fatalf("turn did not reach the witnessed fixture boundary: %+v; effect entries=%d", delivered, len(entries))
	}
	marker, err := os.ReadFile(filepath.Join(root, "effects", delivered.Result.TurnID))
	if err != nil || string(marker) != "71 ahplA" {
		t.Fatalf("independent worker effect: bytes=%q error=%v", marker, err)
	}
	var recovered struct {
		CommandID string `json:"command_id"`
		State     string `json:"state"`
	}
	call(t, client, "command", "command.inspect", map[string]any{
		"target": map[string]string{"owner_id": described.OwnerID}, "key": "duo_dev_turn",
	}, &recovered)
	if recovered.CommandID != delivered.CommandID || recovered.State != "completed" {
		t.Fatalf("original-key inspection mismatched turn: %+v", recovered)
	}
	type snapshotPage struct {
		SnapshotID string            `json:"snapshot_id"`
		Barrier    map[string]string `json:"barrier"`
		Items      []struct {
			Blocks []struct {
				Content string `json:"content"`
				Source  string `json:"source"`
			} `json:"blocks"`
		} `json:"items"`
		NextPage string `json:"next_page"`
	}
	var page snapshotPage
	call(t, client, "snapshot", "conversation.snapshot", map[string]any{
		"target": target, "page_size": 1,
	}, &page)
	if len(page.Items) != 1 || len(page.Items[0].Blocks) != 1 || page.Items[0].Blocks[0].Content != "Alpha 17" || page.NextPage == "" {
		t.Fatalf("first bounded page lost the input: %+v", page)
	}
	var next snapshotPage
	call(t, client, "snapshot", "conversation.snapshot", map[string]any{
		"target": target, "page_size": 1, "page_token": page.NextPage,
	}, &next)
	if next.SnapshotID != page.SnapshotID || !reflect.DeepEqual(next.Barrier, page.Barrier) ||
		len(next.Items) != 1 || len(next.Items[0].Blocks) != 1 || next.Items[0].Blocks[0].Content != "71 ahplA" || next.NextPage != "" {
		t.Fatalf("second page changed snapshot or output: %+v", next)
	}
	var followed struct {
		Events []subjectDocument `json:"events"`
		Cursor map[string]string `json:"cursor"`
	}
	follow := map[string]any{"target": target, "cursor": first.Barrier, "limit": 8}
	response, err := client.Call(ctx, "events.follow", follow)
	if err != nil || json.Unmarshal(response, &followed) != nil {
		t.Fatalf("follow after snapshot: %v", err)
	}
	if len(followed.Events) != 4 {
		t.Fatalf("follow returned %d events, want 4", len(followed.Events))
	}
	kinds := []string{"turn.admitted", "conversation.recorded", "conversation.recorded", "turn.completed"}
	ids := map[string]bool{}
	for i, event := range followed.Events {
		var value struct {
			EventID  string            `json:"event_id"`
			Position string            `json:"position"`
			Kind     string            `json:"kind"`
			Scope    map[string]string `json:"scope"`
		}
		if event.Schema != "agent.harness/v0" || event.Kind != "event" || json.Unmarshal(event.Value, &value) != nil ||
			value.Kind != kinds[i] || value.Position != fmt.Sprint(i+2) || value.EventID == "" || ids[value.EventID] ||
			value.Scope["owner_id"] != described.OwnerID || value.Scope["session_id"] != observed.SessionID {
			t.Fatalf("event %d lost ordered scoped identity: %+v", i, event)
		}
		ids[value.EventID] = true
	}
	var replay struct {
		Events []subjectDocument `json:"events"`
	}
	replayed, err := client.Call(ctx, "events.follow", follow)
	if err != nil || json.Unmarshal(replayed, &replay) != nil || !reflect.DeepEqual(replay.Events, followed.Events) {
		t.Fatalf("replay changed scoped event IDs or content: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "effects")); err != nil || len(entries) != 1 {
		t.Fatalf("replay caused or concealed a duplicate fixture effect: entries=%d error=%v", len(entries), err)
	}
	if strings.TrimSpace(followed.Cursor["position"]) != "5" {
		t.Fatalf("follow cursor=%v, want position 5", followed.Cursor)
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("stop owner before restart: %v", err)
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("owner shutdown before restart: %v", err)
	}
	startSubject(t, program, root)
	var afterRestart struct {
		OwnerID          string `json:"owner_id"`
		SessionID        string `json:"session_id"`
		IncarnationID    string `json:"incarnation_id"`
		Revision         string `json:"revision"`
		IncarnationState string `json:"incarnation_state"`
	}
	call(t, client, "session", "session.inspect", map[string]any{"target": target}, &afterRestart)
	if afterRestart.OwnerID != described.OwnerID || afterRestart.SessionID != observed.SessionID ||
		afterRestart.IncarnationID == observed.IncarnationID || afterRestart.Revision == observed.Revision ||
		afterRestart.IncarnationState != "live" {
		t.Fatalf("restart changed owner/session identity or retained the stale incarnation: %+v", afterRestart)
	}
	call(t, client, "command", "command.inspect", map[string]any{
		"target": map[string]string{"owner_id": described.OwnerID}, "key": "duo_dev_turn",
	}, &recovered)
	if recovered.CommandID != delivered.CommandID || recovered.State != "completed" {
		t.Fatalf("restart lost original command identity: %+v", recovered)
	}
	var replaced struct {
		Events []subjectDocument `json:"events"`
		Cursor map[string]string `json:"cursor"`
	}
	response, err = client.Call(ctx, "events.follow", map[string]any{
		"target": target, "cursor": followed.Cursor, "limit": 8,
	})
	if err != nil || json.Unmarshal(response, &replaced) != nil || len(replaced.Events) != 1 || replaced.Cursor["position"] != "6" {
		t.Fatalf("restart follow lost the successor event: %v; events=%+v", err, replaced)
	}
	var incarnationEvent struct {
		Kind     string            `json:"kind"`
		Position string            `json:"position"`
		Scope    map[string]string `json:"scope"`
		Data     struct {
			IncarnationID string `json:"incarnation_id"`
		} `json:"data"`
	}
	if replaced.Events[0].Schema != "agent.harness/v0" || replaced.Events[0].Kind != "event" ||
		json.Unmarshal(replaced.Events[0].Value, &incarnationEvent) != nil ||
		incarnationEvent.Kind != "incarnation.replaced" || incarnationEvent.Position != "6" ||
		incarnationEvent.Data.IncarnationID != afterRestart.IncarnationID ||
		incarnationEvent.Scope["session_id"] != observed.SessionID {
		t.Fatalf("replacement event does not match the scoped new incarnation: %+v", replaced.Events[0])
	}
	var repeatedTurn struct {
		CommandID string `json:"command_id"`
		State     string `json:"state"`
	}
	call(t, client, "command", "turn.submit", map[string]any{"write": turnWrite}, &repeatedTurn)
	if repeatedTurn.CommandID != delivered.CommandID || repeatedTurn.State != "completed" {
		t.Fatalf("retry of original key after restart lost its command: %+v", repeatedTurn)
	}
	staleWrite := map[string]any{}
	for key, value := range turnWrite {
		staleWrite[key] = value
	}
	staleWrite["idempotency_key"] = "duo_dev_stale_incarnation"
	var stale struct {
		Class  string `json:"class"`
		Effect string `json:"effect"`
	}
	call(t, client, "refusal", "turn.submit", map[string]any{"write": staleWrite}, &stale)
	if stale.Class != "conflict" || stale.Effect != "no_effect" {
		t.Fatalf("stale incarnation accepted new input after restart: %+v", stale)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "effects")); err != nil || len(entries) != 1 {
		t.Fatalf("restart replay or stale write duplicated the fixture effect: entries=%d error=%v", len(entries), err)
	}
	// The dev test proves only the enumerated one-caller operations. Duo's
	// prompt command model, product protocol-owned role and public binding
	// remain unclaimed; none is inferred from this association.
}
