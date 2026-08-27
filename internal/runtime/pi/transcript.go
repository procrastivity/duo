package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// TranscriptSchemaVersion is the pi session-JSONL schema version this parser
// understands, read from the header line's "version". A file that declares
// another version is refused rather than parsed on the assumption that the
// entry shapes did not change.
const TranscriptSchemaVersion = 3

// defaultConversationLimit is the batch size for a request that names none
// (§5.3 leaves the default to the adapter). It bounds one response; a caller
// that wants everything follows NextCursor.
const defaultConversationLimit = 200

// maxTranscriptLine bounds one JSONL line. Real entries carry whole assistant
// messages plus reasoning blobs, well past bufio.Scanner's 64 KiB default.
const maxTranscriptLine = 8 << 20

// errHeaderMismatch is the condition-reason sentinel for a transcript whose
// header session id does not match the requested session. checkHeader wraps
// it so ObserveCondition can map via errors.Is without string-matching.
var errHeaderMismatch = errors.New("transcript header does not match session")

// Pi's own message roles, used verbatim as the ConversationTurn role
// vocabulary. Inventing Duo-side role names here would be a second, unpinned
// vocabulary to keep in sync; §5.3 does not fix one.
const (
	RoleUser       = "user"
	RoleAssistant  = "assistant"
	RoleToolResult = "toolResult"
)

// transcriptHeader is the first line of every pi session file. It is the only
// place the session id appears (entries carry entry ids, not session ids), so
// reading it is how this adapter proves a file belongs to the session it was
// asked about.
type transcriptHeader struct {
	Type          string `json:"type"`
	Version       int    `json:"version"`
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Cwd           string `json:"cwd"`
	ParentSession string `json:"parentSession"`
}

// transcriptEntry is one JSONL line after the header. Only the fields this
// Stage-1 conversation projection needs are decoded: model_change /
// thinking_level_change entries and the usage, cost, and provider identity on
// assistant messages are runtime-configuration and usage concerns that §5.3
// assigns to other providers.
type transcriptEntry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// ReadConversation implements runtime.ConversationProvider.
//
// The projection rule is one turn per pi message entry that has text, with
// pi's own role as the turn's role. Two block kinds are deliberately dropped:
//
//   - "thinking" blocks, because ConversationTurn has no content-kind
//     discriminator, and emitting reasoning as the assistant's Text would
//     present it as something the agent said;
//   - "toolCall" blocks, which carry a tool name and arguments and no text at
//     all, so there is nothing for a Text-only turn to hold. The matching
//     "toolResult" entry does carry text and is projected.
//
// Both omissions are content the shape cannot express, not content this
// adapter cannot read; see docs/adapters/decisions.md.
//
// The cursor is an opaque line count into the file. Pi appends — `pi -c`
// resumes into the same file with no new header — so a line count stays valid
// across a resume. A fork is a different file with a different session id, and
// pi copies the parent's entries into it, so reading a forked transcript
// returns the inherited history too.
func (r *Runtime) ReadConversation(ctx context.Context, req runtime.ConversationReadRequest) (runtime.ConversationBatch, error) {
	if req.ExternalAgentSessionID == "" {
		return runtime.ConversationBatch{}, fmt.Errorf(
			"pi: ReadConversation needs an external agent-session ID: a transcript path alone does not scope a read")
	}
	offset, err := cursorOffset(req.After)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}

	// Peel path-shaped ExternalAgentSessionID before checkHeader (I-12).
	sessionID := req.ExternalAgentSessionID
	if peeled := SessionIDFromTranscriptName(sessionID); peeled != "" {
		sessionID = peeled
	}

	path := req.TranscriptID
	if path == "" {
		path, err = r.resolveTranscript(sessionID, "", "")
		if err != nil {
			return runtime.ConversationBatch{}, err
		}
	}

	turns, totalLines, err := readTurns(ctx, path, sessionID)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultConversationLimit
	}

	start := 0
	for start < len(turns) && turns[start].line <= offset {
		start++
	}
	end := start + limit
	if end > len(turns) {
		end = len(turns)
	}

	batch := make([]runtime.ConversationTurn, 0, end-start)
	for _, t := range turns[start:end] {
		batch = append(batch, t.turn)
	}

	complete := end == len(turns)
	next := offset
	if complete {
		if totalLines > next {
			next = totalLines
		}
	} else {
		next = turns[end-1].line
	}

	return runtime.ConversationBatch{
		Turns:      batch,
		NextCursor: strconv.Itoa(next),
		Complete:   complete,
	}, nil
}

// turnAt pairs a projected turn with the 1-based line it came from, which is
// what the cursor counts.
type turnAt struct {
	turn runtime.ConversationTurn
	line int
}

// readTurns parses the whole file and returns every projected turn plus the
// line count. Stage 1 rescans from the start on every call: the cursor is a
// position, not a saved file offset, and a pi transcript is small enough
// (tens to low thousands of lines) that a rescan is cheaper than carrying
// open-file state across calls. A live-tail implementation is a Stage 2
// concern, alongside the streaming contract that would justify it.
func readTurns(ctx context.Context, path, sessionID string) ([]turnAt, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("pi: open transcript: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), maxTranscriptLine)

	var (
		turns []turnAt
		line  int
	)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		line++
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}

		if line == 1 {
			if err := checkHeader(raw, path, sessionID); err != nil {
				return nil, 0, err
			}
			continue
		}

		var e transcriptEntry
		if json.Unmarshal(raw, &e) != nil {
			// A malformed line is skipped, not fatal: pi appends entries
			// whole, so the realistic cause is a partial write at the tail
			// of a live file, which the next read picks up.
			continue
		}
		if e.Type != "message" {
			continue
		}
		turn, ok := projectTurn(e, line)
		if !ok {
			continue
		}
		turns = append(turns, turnAt{turn: turn, line: line})
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("pi: read transcript %s: %w", path, err)
	}
	if line == 0 {
		return nil, 0, fmt.Errorf("pi: transcript %s is empty", path)
	}
	return turns, line, nil
}

func checkHeader(raw []byte, path, sessionID string) error {
	var h transcriptHeader
	if err := json.Unmarshal(raw, &h); err != nil {
		return fmt.Errorf("pi: transcript %s: header line is not JSON: %w", path, err)
	}
	if h.Type != "session" {
		return fmt.Errorf("pi: transcript %s: first entry is %q, not a session header", path, h.Type)
	}
	if h.Version != TranscriptSchemaVersion {
		return fmt.Errorf("pi: transcript %s: schema version %d, this build parses version %d",
			path, h.Version, TranscriptSchemaVersion)
	}
	if h.ID != sessionID {
		return fmt.Errorf("%w: pi: transcript %s belongs to session %s, not %s",
			errHeaderMismatch, path, h.ID, sessionID)
	}
	return nil
}

func projectTurn(e transcriptEntry, line int) (runtime.ConversationTurn, bool) {
	switch e.Message.Role {
	case RoleUser, RoleAssistant, RoleToolResult:
	default:
		return runtime.ConversationTurn{}, false
	}

	var parts []string
	for _, b := range e.Message.Content {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return runtime.ConversationTurn{}, false
	}

	id := e.ID
	if id == "" {
		id = "line-" + strconv.Itoa(line)
	}
	at, err := time.Parse(time.RFC3339, e.Timestamp)
	if err != nil {
		at = time.Time{}
	}
	return runtime.ConversationTurn{
		ID:   id,
		Role: e.Message.Role,
		// Multiple text blocks in one entry are separate paragraphs of the
		// same message, joined rather than emitted as separate turns: they
		// share one entry id and one timestamp.
		Text: strings.Join(parts, "\n"),
		At:   at,
	}, true
}

func cursorOffset(after string) (int, error) {
	if after == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(after)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("pi: invalid cursor %q", after)
	}
	return n, nil
}
