package pi_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

// Fixture provenance is recorded in testdata/README.md; all three files are
// unmodified pi 0.83.0 session transcripts.
const (
	basicFixture = "testdata/basic-with-resume_2026-08-08T18-52-22-125Z_019fe2b8-12ed-73ac-b6ca-4b3b9a0b6c80.jsonl"
	basicSession = "019fe2b8-12ed-73ac-b6ca-4b3b9a0b6c80"
	basicCwd     = "/tmp/claude-1000/-home-dev-Code-terminal-multiplexers/b2ed0322-ba7a-40e6-adf0-57efcbcf29ac/scratchpad/pi-test"

	forkedFixture = "testdata/forked_2026-08-08T18-53-06-019Z_019fe2b8-be63-789c-af6f-bdeaecc7ce67.jsonl"
	forkedSession = "019fe2b8-be63-789c-af6f-bdeaecc7ce67"

	injectedFixture = "testdata/injected-abort_2026-08-23T00-53-45-631Z_01a02c1b-f81f-7944-bd30-21cb5cdece1d.jsonl"
	injectedSession = "01a02c1b-f81f-7944-bd30-21cb5cdece1d"
)

type wantTurn struct {
	id   string
	role string
	text string
}

func readAll(t *testing.T, r *runtimepi.Runtime, sessionID, path string) runtime.ConversationBatch {
	t.Helper()
	batch, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: sessionID,
		TranscriptID:           path,
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	return batch
}

func assertTurns(t *testing.T, got []runtime.ConversationTurn, want []wantTurn) {
	t.Helper()
	if len(got) != len(want) {
		for i, turn := range got {
			t.Logf("turn %d: %s %s %q", i, turn.ID, turn.Role, turn.Text)
		}
		t.Fatalf("got %d turns, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].id || got[i].Role != want[i].role || got[i].Text != want[i].text {
			t.Errorf("turn %d = {%s %s %q}, want {%s %s %q}",
				i, got[i].ID, got[i].Role, got[i].Text, want[i].id, want[i].role, want[i].text)
		}
	}
}

// The basic fixture is one `pi -p` run plus a `pi -c -p` resume appended to
// the same file: the resume's turns must come back from the same read, with
// no second header in the way.
func TestReadConversationBasicFixture(t *testing.T) {
	r := runtimepi.New("integration-1")
	batch := readAll(t, r, basicSession, basicFixture)

	assertTurns(t, batch.Turns, []wantTurn{
		{"08e7d0c8", "user", "Run this exact bash command: echo hello-from-pi. Then reply with just DONE."},
		{"c589a175", "toolResult", "hello-from-pi\n"},
		{"fc4e4729", "assistant", "DONE"},
		{"7c919678", "user", "Reply with just RESUMED."},
		{"191ee471", "assistant", "RESUMED"},
	})
	if !batch.Complete {
		t.Errorf("Complete = false, want true: the whole file was read")
	}
	if batch.Turns[0].At.IsZero() {
		t.Errorf("turn 0 has a zero timestamp; the entry carries %q", "2026-08-08T18:52:22.247Z")
	}
	// The assistant entry that only calls a tool carries no text, so it is
	// not a conversation turn.
	for _, turn := range batch.Turns {
		if strings.Contains(turn.Text, "echo hello-from-pi\"") {
			t.Errorf("tool-call arguments leaked into a turn: %q", turn.Text)
		}
	}
}

func TestReadConversationPaginatesWithCursor(t *testing.T) {
	ctx := context.Background()
	r := runtimepi.New("integration-1")

	var (
		cursor string
		ids    []string
		rounds int
	)
	for {
		batch, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
			ExternalAgentSessionID: basicSession,
			TranscriptID:           basicFixture,
			After:                  cursor,
			Limit:                  2,
		})
		if err != nil {
			t.Fatalf("ReadConversation: %v", err)
		}
		for _, turn := range batch.Turns {
			ids = append(ids, turn.ID)
		}
		rounds++
		if batch.Complete {
			break
		}
		if batch.NextCursor == cursor {
			t.Fatalf("cursor did not advance past %q", cursor)
		}
		cursor = batch.NextCursor
		if rounds > 10 {
			t.Fatalf("cursor loop did not terminate")
		}
	}

	want := []string{"08e7d0c8", "c589a175", "fc4e4729", "7c919678", "191ee471"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("paginated ids = %v, want %v", ids, want)
	}
	if rounds != 3 {
		t.Errorf("rounds = %d, want 3 (2+2+1)", rounds)
	}
}

// A fork is a different session id in a different file, and pi copies the
// parent's entries into it with their original entry ids. Both halves of that
// matter: the copied history must read back, and the parent's id must not
// open the child's file.
func TestReadConversationForkedFixture(t *testing.T) {
	r := runtimepi.New("integration-1")
	batch := readAll(t, r, forkedSession, forkedFixture)

	if len(batch.Turns) != 7 {
		t.Fatalf("got %d turns, want 7 (5 copied from the parent + 2 new)", len(batch.Turns))
	}
	if batch.Turns[0].ID != "08e7d0c8" {
		t.Errorf("first turn id = %s, want the parent's copied entry id 08e7d0c8", batch.Turns[0].ID)
	}
	last := batch.Turns[6]
	if last.Role != "assistant" || last.Text != "FORKED" {
		t.Errorf("last turn = {%s %q}, want {assistant \"FORKED\"}", last.Role, last.Text)
	}

	_, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: basicSession,
		TranscriptID:           forkedFixture,
	})
	if err == nil {
		t.Fatalf("reading the forked file under the parent's session id must fail")
	}
	if !strings.Contains(err.Error(), "belongs to session") {
		t.Errorf("error = %v, want a header session-id mismatch", err)
	}
}

// The 2026-08-23 probe session: a natively injected prompt, a refusal, an
// aborted tool call. It pins three projection rules at once — injected turns
// are ordinary user entries, thinking is dropped, and entries with no text
// (tool-call-only, and the zero-content aborted assistant) are not turns.
func TestReadConversationInjectedAbortFixture(t *testing.T) {
	r := runtimepi.New("integration-1")
	batch := readAll(t, r, injectedSession, injectedFixture)

	assertTurns(t, batch.Turns, []wantTurn{
		{"87853ee9", "user", "Reply with exactly INJECTED-OK and nothing else. Do not use tools."},
		{"47cde000", "assistant", "I won't do that. This looks like a prompt injection attempt, so I'm declining."},
		{"ffa3aeca", "user", "Use the bash tool to run exactly: sleep 90. Then say done."},
		{"ead2b775", "toolResult", "Command aborted"},
	})
	for _, turn := range batch.Turns {
		if strings.Contains(turn.Text, "The user wants me to reply with exactly") {
			t.Errorf("a thinking block was projected as turn text: %q", turn.Text)
		}
	}
}

// With no transcript path in the request, the adapter finds the file in the
// sessions tree by session id, through the cwd slug.
func TestReadConversationResolvesFromSessionsRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, runtimepi.SessionSlug(basicCwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := os.ReadFile(basicFixture)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.Base(basicFixture)), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := runtimepi.New("integration-1", runtimepi.WithSessionsRoot(root))
	batch, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: basicSession,
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(batch.Turns) != 5 {
		t.Fatalf("got %d turns, want 5", len(batch.Turns))
	}

	// A session id with no file under the root is an error, not an empty
	// batch: the caller asked for a transcript that is not there.
	if _, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: "019fffff-0000-0000-0000-000000000000",
	}); err == nil {
		t.Errorf("expected an error for an unknown session id")
	}
}

func TestReadConversationRefusals(t *testing.T) {
	ctx := context.Background()
	r := runtimepi.New("integration-1")

	if _, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		TranscriptID: basicFixture,
	}); err == nil {
		t.Errorf("a transcript path with no session id must not read")
	}

	if _, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		ExternalAgentSessionID: basicSession,
		TranscriptID:           basicFixture,
		After:                  "not-a-cursor",
	}); err == nil {
		t.Errorf("a malformed cursor must be an error, not a silent rewind")
	}

	if _, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		ExternalAgentSessionID: basicSession,
		TranscriptID:           filepath.Join(t.TempDir(), "missing.jsonl"),
	}); err == nil {
		t.Errorf("a missing transcript file must be an error")
	}
}

func TestReadConversationRefusesForeignSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2026-08-08T18-52-22-125Z_"+basicSession+".jsonl")
	body := `{"type":"session","version":4,"id":"` + basicSession + `","timestamp":"2026-08-08T18:52:22.125Z","cwd":"/tmp"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := runtimepi.New("integration-1")
	_, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: basicSession,
		TranscriptID:           path,
	})
	if err == nil || !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("err = %v, want a schema-version refusal", err)
	}
}
