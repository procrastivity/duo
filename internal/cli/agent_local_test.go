package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/protocolowned/devclient"
)

func TestAgentLocalTwoTurns(t *testing.T) {
	program := os.Getenv("DUO_AGENT_PROGRAM")
	if program == "" {
		t.Skip("requires separate pinned duo-agent checkout: DUO_AGENT_PROGRAM=/absolute/path/own_agent.py")
	}
	if !filepath.IsAbs(program) {
		t.Fatal("DUO_AGENT_PROGRAM must be absolute")
	}
	sum := sha256.New()
	for i, name := range []string{"own_agent.py", "deterministic_worker.py", "opencode_worker.py", "human_console.py"} {
		if i != 0 {
			_, _ = sum.Write([]byte{0})
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(program), name))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = sum.Write(data)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != "2971734021e91eabae721cffa818530814f31e68be81d4aee4e852bd9967cee3" {
		t.Fatalf("external agent source changed: %s; requalify before updating pin", got)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "owner")
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command(python, "-I", "-B", program, "--state-dir", root)
	process.Env = []string{"PYTHONDONTWRITEBYTECODE=1"}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Process.Signal(syscall.SIGTERM)
		if err := process.Wait(); err != nil {
			t.Errorf("agent shutdown: %v", err)
		}
	})
	limit := time.Now().Add(3 * time.Second)
	for time.Now().Before(limit) {
		if _, err := os.Stat(filepath.Join(root, "agent.sock")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	token, err := os.ReadFile(filepath.Join(root, "token"))
	if err != nil {
		t.Fatal(err)
	}
	client := devclient.Client{Socket: filepath.Join(root, "agent.sock"), Token: string(token)}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.Call(ctx, "protocol.describe", nil)
	if err != nil {
		t.Fatal(err)
	}
	var described struct {
		Value struct {
			OwnerID string `json:"owner_id"`
		} `json:"value"`
	}
	if err := json.Unmarshal(result, &described); err != nil || described.Value.OwnerID == "" {
		t.Fatalf("describe: %s %v", result, err)
	}
	if code, _, _ := runSession(t, "agent-local", "create", "--state-dir", root); code != exitcode.Usage {
		t.Fatalf("private CLI exposed without opt-in: code %d", code)
	}
	t.Setenv("DUO_AGENT_LOCAL", "1")
	deadline := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second).Format("2006-01-02T15:04:05Z")
	create := []string{"agent-local", "create", "--state-dir", root, "--label", "Duo local", "--key", "duo_local_create", "--deadline", deadline}
	code, out, stderr := runSession(t, create...)
	if code != exitcode.Success || stderr != "" {
		t.Fatalf("create: code=%d stdout=%q stderr=%q", code, out, stderr)
	}
	var created struct {
		OwnerSessionID string `json:"owner_session_id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.OwnerSessionID == "" {
		t.Fatalf("create result: %s %v", out, err)
	}
	if againCode, again, againErr := runSession(t, create...); againCode != exitcode.Success || again != out || againErr != "" {
		t.Fatalf("original create key changed meaning: %d %q %q", againCode, again, againErr)
	}
	connect := []string{"agent-local", "connect", created.OwnerSessionID, "--state-dir", root, "--workspace", t.TempDir()}
	code, out, stderr = runSession(t, connect...)
	if code != exitcode.Success || stderr != "" {
		t.Fatalf("connect: code=%d stdout=%q stderr=%q", code, out, stderr)
	}
	var connected struct {
		DuoSessionID   string `json:"duo_session_id"`
		OwnerSessionID string `json:"owner_session_id"`
	}
	if err := json.Unmarshal([]byte(out), &connected); err != nil || connected.DuoSessionID == "" || connected.OwnerSessionID != created.OwnerSessionID {
		t.Fatalf("connect identities: %s %v", out, err)
	}
	code, again, stderr := runSession(t, connect...)
	if code != exitcode.Success || again != out || stderr != "" {
		t.Fatalf("repeated connect created another Duo session: code=%d stdout=%q stderr=%q", code, again, stderr)
	}
	a, closer, err := openReadAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := a.Session(domain.SessionID(connected.DuoSessionID))
	if !ok || session.Attachment != "" || len(a.Attachments(session.ID)) != 0 || len(a.Sessions()) != 1 {
		t.Fatalf("connect did not create exactly one hostless Duo session: %+v", session)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	turn := []string{"agent-local", "turn", connected.DuoSessionID, "--state-dir", root, "--text", "Alpha 17", "--key", "duo_local_turn"}
	localTarget := map[string]string{"owner_id": described.Value.OwnerID, "session_id": created.OwnerSessionID}
	result, err = client.Call(ctx, "local.human.begin", map[string]any{"target": localTarget})
	if err != nil {
		t.Fatal(err)
	}
	var human struct {
		Holder string `json:"holder"`
	}
	if err := json.Unmarshal(result, &human); err != nil || human.Holder == "" {
		t.Fatalf("human writer did not take a lease: %s %v", result, err)
	}
	code, _, stderr = runSession(t, turn...)
	if code == exitcode.Success || !strings.Contains(stderr, "refused conflict without effect") {
		t.Fatalf("Duo turn ignored the private owner's live writer: code=%d stderr=%q", code, stderr)
	}
	a, closer, err = openReadAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	blocked, ok := a.CommandByIdempotency("agent-local@"+described.Value.OwnerID, connected.DuoSessionID, "duo_local_turn")
	if !ok || blocked.State != domain.ResponsibilityQueued || len(blocked.Attempts) != 1 ||
		blocked.Attempts[0].EffectCertainty != domain.EffectNoEffect {
		t.Fatalf("Duo did not record the owner no-effect refusal: %+v", blocked)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "effects")); err != nil || len(entries) != 0 {
		t.Fatalf("blocked turn wrote a fixture effect: %d %v", len(entries), err)
	}
	if _, err := client.Call(ctx, "local.human.end", map[string]any{"target": localTarget, "holder": human.Holder}); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = runSession(t, turn...)
	if code != exitcode.Success || stderr != "" {
		t.Fatalf("turn: code=%d stdout=%q stderr=%q", code, out, stderr)
	}
	var delivered struct {
		Output     string `json:"output"`
		TurnID     string `json:"turn_id"`
		Command    string `json:"owner_command_id"`
		DuoCommand string `json:"duo_command_id"`
	}
	if err := json.Unmarshal([]byte(out), &delivered); err != nil || delivered.Output != "71 ahplA" ||
		delivered.TurnID == "" || delivered.Command == "" || delivered.DuoCommand != string(blocked.ID) {
		t.Fatalf("turn result: %s %v", out, err)
	}
	a, closer, err = openReadAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstCommand, ok := a.Command(blocked.ID)
	if !ok || firstCommand.State != domain.ResponsibilityDelivered || len(firstCommand.Attempts) != 2 ||
		firstCommand.Attempts[1].PathKind != domain.PromptPathRuntime || firstCommand.Attempts[1].RecordedResult != "delivered" {
		t.Fatalf("Duo did not prove its second attempt delivered: %+v", firstCommand)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(root, "effects", delivered.TurnID))
	if err != nil || string(marker) != "71 ahplA" {
		t.Fatalf("independent fixture effect: %q %v", marker, err)
	}
	code, repeated, stderr := runSession(t, turn...)
	if code != exitcode.Success || repeated != out || stderr != "" {
		t.Fatalf("repeat should inspect original key, not write: %d %q %q", code, repeated, stderr)
	}
	changed := append([]string(nil), turn...)
	changed[6] = "Beta 17"
	code, _, stderr = runSession(t, changed...)
	if code == exitcode.Success || !strings.Contains(stderr, "command.idempotency_conflict") {
		t.Fatalf("changed input under original key should not be presented as success: %d %q", code, stderr)
	}
	entries, err := os.ReadDir(filepath.Join(root, "effects"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("repeat/changed-key attempt duplicated effect: %d %v", len(entries), err)
	}
	second := []string{
		"agent-local", "turn", connected.DuoSessionID, "--state-dir", root,
		"--text", "Beta 29", "--key", "duo_local_second",
	}
	// Simulate a process ending after the owner completed, before Duo could
	// close its already durable attempt. The CLI must inspect, not reissue.
	a, closer, err = openWriteAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, ok = a.Session(domain.SessionID(connected.DuoSessionID))
	if !ok {
		t.Fatal("connected Duo session missing")
	}
	accepted, err := a.AcceptPrompt(ctx, domain.AcceptPromptRequest{
		Session: session.ID, Instance: session.Current,
		Actor: "agent-local@" + described.Value.OwnerID, IdempotencyKey: "duo_local_second",
		CanonicalDigest: promptCanonicalDigest("Beta 29"), ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondAttempt, err := a.CreateAttempt(ctx, accepted.Command.ID, "agent-local@"+described.Value.OwnerID, domain.PromptPathRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	var ownerSession struct {
		Value struct {
			IncarnationID string `json:"incarnation_id"`
			Revision      string `json:"revision"`
		} `json:"value"`
	}
	result, err = client.Call(ctx, "session.inspect", map[string]any{"target": localTarget})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(result, &ownerSession); err != nil {
		t.Fatal(err)
	}
	result, err = client.Call(ctx, "turn.submit", map[string]any{"write": map[string]any{
		"operation": "turn.submit", "target": map[string]string{
			"owner_id": described.Value.OwnerID, "session_id": created.OwnerSessionID,
			"incarnation_id": ownerSession.Value.IncarnationID,
		}, "idempotency_key": "duo_local_second", "deadline": deadline, "queue_policy": "require_ready",
		"payload":       map[string]any{"blocks": []any{map[string]string{"type": "text", "content": "Beta 29"}}},
		"preconditions": map[string]string{"session_revision": ownerSession.Value.Revision}, "grant": "local",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var ownerTurn struct {
		Value struct {
			State string `json:"state"`
		} `json:"value"`
	}
	if err := json.Unmarshal(result, &ownerTurn); err != nil || ownerTurn.Value.State != "completed" {
		t.Fatalf("owner did not finish the crash-gap turn: %v", err)
	}
	code, secondOut, stderr := runSession(t, second...)
	if code != exitcode.Success || stderr != "" {
		t.Fatalf("second turn: code=%d stdout=%q stderr=%q", code, secondOut, stderr)
	}
	var secondDelivered struct {
		Output     string `json:"output"`
		TurnID     string `json:"turn_id"`
		Command    string `json:"owner_command_id"`
		DuoCommand string `json:"duo_command_id"`
	}
	if err := json.Unmarshal([]byte(secondOut), &secondDelivered); err != nil ||
		secondDelivered.Output != "92 ateB" || secondDelivered.TurnID == "" ||
		secondDelivered.TurnID == delivered.TurnID || secondDelivered.Command == delivered.Command ||
		secondDelivered.DuoCommand == "" || secondDelivered.DuoCommand == delivered.DuoCommand {
		t.Fatalf("second answer attributed to wrong turn: %q %v", secondOut, err)
	}
	marker, err = os.ReadFile(filepath.Join(root, "effects", secondDelivered.TurnID))
	if err != nil || string(marker) != secondDelivered.Output {
		t.Fatalf("independent second effect: %q %v", marker, err)
	}
	a, closer, err = openReadAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recovered, ok := a.Command(accepted.Command.ID)
	if !ok || recovered.State != domain.ResponsibilityDelivered || len(recovered.Attempts) != 1 ||
		recovered.Attempts[0].ID != secondAttempt {
		t.Fatalf("Duo did not close the original attempt: %+v", recovered)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if code, replay, _ := runSession(t, turn...); code != exitcode.Success || replay != out {
		t.Fatalf("first-key replay after second turn: %d %q", code, replay)
	}
	if code, replay, _ := runSession(t, second...); code != exitcode.Success || replay != secondOut {
		t.Fatalf("second-key replay: %d %q", code, replay)
	}
	wrongSecond := append([]string(nil), second...)
	wrongSecond[6] = "Alpha 17" // matches the first input, not this key's turn ID
	if code, _, stderr := runSession(t, wrongSecond...); code == exitcode.Success || !strings.Contains(stderr, "command.idempotency_conflict") {
		t.Fatalf("second key was misattributed to the first input: %d %q", code, stderr)
	}
	third := []string{
		"agent-local", "turn", connected.DuoSessionID, "--state-dir", root,
		"--text", "Gamma 35", "--key", "duo_local_third",
	}
	if code, _, stderr := runSession(t, third...); code == exitcode.Success || !strings.Contains(stderr, "at most two owner turns") {
		t.Fatalf("third turn must refuse before an effect: %d %q", code, stderr)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "effects")); err != nil || len(entries) != 2 {
		t.Fatalf("replay/third attempt duplicated fixture effect: %d %v", len(entries), err)
	}
	if strings.Contains(out+stderr, string(token)) {
		t.Fatal("private token leaked to CLI output")
	}
	a, closer, err = openWriteAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, ok = a.Session(domain.SessionID(connected.DuoSessionID))
	if !ok || session.Current == "" {
		t.Fatal("connected Duo instance missing")
	}
	if err := a.Stop(ctx, session.Current, "test", "stale association refusal"); err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runSession(t, turn...); code == exitcode.Success || !strings.Contains(stderr, "instance is not live") {
		t.Fatalf("stopping Duo instance allowed a turn: %d %q", code, stderr)
	}
	if code, _, stderr := runSession(t, connect...); code == exitcode.Success || !strings.Contains(stderr, "association is not live") {
		t.Fatalf("stopping Duo instance allowed a duplicate association: %d %q", code, stderr)
	}
}

func TestAgentLocalUnknownEffectIsNotRetried(t *testing.T) {
	program := os.Getenv("DUO_AGENT_PROGRAM")
	if program == "" {
		t.Skip("requires separate pinned duo-agent checkout")
	}
	t.Setenv("DUO_AGENT_LOCAL", "1")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "owner")
	start := func() *exec.Cmd {
		process := exec.Command("/usr/bin/python3", "-I", "-B", program, "--state-dir", root,
			"--test-worker-delay-after-effect", "2")
		process.Env = []string{"PYTHONDONTWRITEBYTECODE=1"}
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
		limit := time.Now().Add(3 * time.Second)
		for time.Now().Before(limit) {
			conn, err := net.DialTimeout("unix", filepath.Join(root, "agent.sock"), 50*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return process
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("private owner did not bind")
		return nil
	}
	process := start()
	t.Cleanup(func() {
		if process.ProcessState == nil {
			_ = process.Process.Signal(syscall.SIGTERM)
			_ = process.Wait()
		}
	})
	deadline := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second).Format("2006-01-02T15:04:05Z")
	code, raw, stderr := runSession(t, "agent-local", "create", "--state-dir", root,
		"--label", "uncertain", "--key", "uncertain_create", "--deadline", deadline)
	if code != exitcode.Success {
		t.Fatalf("create: %d %q", code, stderr)
	}
	var created struct {
		OwnerSessionID string `json:"owner_session_id"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatal(err)
	}
	code, raw, stderr = runSession(t, "agent-local", "connect", created.OwnerSessionID,
		"--state-dir", root, "--workspace", t.TempDir())
	if code != exitcode.Success {
		t.Fatalf("connect: %d %q", code, stderr)
	}
	var connected struct {
		DuoSessionID string `json:"duo_session_id"`
	}
	if err := json.Unmarshal([]byte(raw), &connected); err != nil {
		t.Fatal(err)
	}
	turn := []string{
		"agent-local", "turn", connected.DuoSessionID, "--state-dir", root,
		"--text", "Unknown 37", "--key", "uncertain_turn",
	}
	type turnOutcome struct {
		code   int
		stderr string
	}
	done := make(chan turnOutcome, 1)
	go func() {
		code, _, stderr := runSession(t, turn...)
		done <- turnOutcome{code, stderr}
	}()
	limit := time.Now().Add(3 * time.Second)
	for time.Now().Before(limit) {
		entries, err := os.ReadDir(filepath.Join(root, "effects"))
		if err == nil && len(entries) == 1 {
			break
		}
		select {
		case outcome := <-done:
			t.Fatalf("turn ended before the effect window: %d %q", outcome.code, outcome.stderr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "effects")); err != nil || len(entries) != 1 {
		t.Fatalf("fixture did not reach the post-effect window: %v", err)
	}
	if err := process.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = process.Wait()
	select {
	case outcome := <-done:
		if outcome.code == exitcode.Success {
			t.Fatal("unknown effect was reported delivered")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Duo turn did not exit after the owner died")
	}
	a, closer, err := openReadAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	command, ok := a.CommandByIdempotency("agent-local@"+ownerIDFromFile(t, root), connected.DuoSessionID, "uncertain_turn")
	if !ok || command.State != domain.ResponsibilityFailed || len(command.Attempts) != 1 ||
		command.Attempts[0].EffectCertainty != domain.EffectUnknownEffect {
		t.Fatalf("Duo did not retain unknown effect: %+v", command)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	process = start()
	token, err := os.ReadFile(filepath.Join(root, "token"))
	if err != nil {
		t.Fatal(err)
	}
	client := devclient.Client{Socket: filepath.Join(root, "agent.sock"), Token: string(token)}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := client.Call(ctx, "protocol.describe", nil); err != nil {
		t.Fatalf("restarted owner unavailable: %v", err)
	}
	if code, _, _ := runSession(t, turn...); code == exitcode.Success {
		t.Fatal("original unknown-effect command was retried")
	}
	if entries, err := os.ReadDir(filepath.Join(root, "effects")); err != nil || len(entries) != 1 {
		t.Fatalf("retry duplicated uncertain effect: %v", err)
	}
	// A separate crash gap: Duo's attempt exists before an owner write, but
	// the owner survives only long enough to persist an incomplete command.
	// On replay, inspection must close that attempt as unknown, not leave it
	// attempting forever or mistake the incomplete command for no effect.
	code, raw, stderr = runSession(t, "agent-local", "create", "--state-dir", root,
		"--label", "pending", "--key", "pending_create", "--deadline", deadline)
	if code != exitcode.Success {
		t.Fatalf("second create: %d %q", code, stderr)
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatal(err)
	}
	code, raw, stderr = runSession(t, "agent-local", "connect", created.OwnerSessionID,
		"--state-dir", root, "--workspace", t.TempDir())
	if code != exitcode.Success {
		t.Fatalf("second connect: %d %q", code, stderr)
	}
	if err := json.Unmarshal([]byte(raw), &connected); err != nil {
		t.Fatal(err)
	}
	actor := "agent-local@" + ownerIDFromFile(t, root)
	a, closer, err = openWriteAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := a.Session(domain.SessionID(connected.DuoSessionID))
	if !ok {
		t.Fatal("second Duo session missing")
	}
	accepted, err := a.AcceptPrompt(ctx, domain.AcceptPromptRequest{
		Session: session.ID, Instance: session.Current, Actor: actor, IdempotencyKey: "pending_turn",
		CanonicalDigest: promptCanonicalDigest("Pending 41"), ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateAttempt(ctx, accepted.Command.ID, actor, domain.PromptPathRuntime); err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	target := map[string]string{"owner_id": ownerIDFromFile(t, root), "session_id": created.OwnerSessionID}
	var inspected struct {
		Value struct {
			IncarnationID string `json:"incarnation_id"`
			Revision      string `json:"revision"`
		} `json:"value"`
	}
	result, err := client.Call(ctx, "session.inspect", map[string]any{"target": target})
	if err != nil || json.Unmarshal(result, &inspected) != nil {
		t.Fatalf("second owner session inspection: %s %v", result, err)
	}
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		_, _ = client.Call(context.Background(), "turn.submit", map[string]any{"write": map[string]any{
			"operation": "turn.submit", "target": map[string]string{
				"owner_id": target["owner_id"], "session_id": target["session_id"],
				"incarnation_id": inspected.Value.IncarnationID,
			}, "idempotency_key": "pending_turn", "deadline": deadline, "queue_policy": "require_ready",
			"payload":       map[string]any{"blocks": []any{map[string]string{"type": "text", "content": "Pending 41"}}},
			"preconditions": map[string]string{"session_revision": inspected.Value.Revision}, "grant": "local",
		}})
	}()
	limit = time.Now().Add(3 * time.Second)
	for time.Now().Before(limit) {
		entries, err := os.ReadDir(filepath.Join(root, "effects"))
		if err == nil && len(entries) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "effects")); err != nil || len(entries) != 2 {
		t.Fatalf("second fixture did not reach post-effect window: %v", err)
	}
	if err := process.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = process.Wait()
	select {
	case <-ownerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("owner request did not end after process death")
	}
	process = start()
	pendingTurn := []string{
		"agent-local", "turn", connected.DuoSessionID, "--state-dir", root,
		"--text", "Pending 41", "--key", "pending_turn",
	}
	if code, _, stderr := runSession(t, pendingTurn...); code == exitcode.Success || !strings.Contains(stderr, "not proved completed") {
		t.Fatalf("pending owner command should refuse on replay: %d %q", code, stderr)
	}
	a, closer, err = openReadAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	command, ok = a.Command(accepted.Command.ID)
	if !ok || command.State != domain.ResponsibilityFailed || len(command.Attempts) != 1 ||
		command.Attempts[0].EffectCertainty != domain.EffectUnknownEffect {
		t.Fatalf("Duo left an incomplete owner command attempting: %+v", command)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runSession(t, pendingTurn...); code == exitcode.Success {
		t.Fatal("incomplete original command was retried")
	}
	if entries, err := os.ReadDir(filepath.Join(root, "effects")); err != nil || len(entries) != 2 {
		t.Fatalf("pending-command replay duplicated the effect: %v", err)
	}
}

func ownerIDFromFile(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		OwnerID string `json:"owner_id"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record.OwnerID
}
