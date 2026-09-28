package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func TestAgentLocalOneTurn(t *testing.T) {
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
	if got := hex.EncodeToString(sum.Sum(nil)); got != "b1692fda4b47313efe47689338a6a2f85c3edc3c2752c803c7be07bd3d8d1f91" {
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
		Output  string `json:"output"`
		TurnID  string `json:"turn_id"`
		Command string `json:"owner_command_id"`
	}
	if err := json.Unmarshal([]byte(out), &delivered); err != nil || delivered.Output != "71 ahplA" || delivered.TurnID == "" || delivered.Command == "" {
		t.Fatalf("turn result: %s %v", out, err)
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
	if code == exitcode.Success || !strings.Contains(stderr, "output not observed") {
		t.Fatalf("changed input under original key should not be presented as success: %d %q", code, stderr)
	}
	entries, err := os.ReadDir(filepath.Join(root, "effects"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("repeat/changed-key attempt duplicated effect: %d %v", len(entries), err)
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
