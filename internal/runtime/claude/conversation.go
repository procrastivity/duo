package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// maxLineSize bounds the JSONL scanner's line buffer. Claude Code writes
// large single-line entries (skill_listing and agent_listing_delta
// attachments in the ported fixtures run past bufio.Scanner's default 64KB
// token limit), so the buffer has to be generous. 16MiB comfortably
// covers every fixture in testdata/claude and the largest entries notes/16
// and notes/06 describe.
const maxLineSize = 16 << 20

// claudeEntry covers the envelope fields ReadConversation needs from any
// JSONL line. Ported from apps/transcript-tail/adapter_claude.go's
// claudeEntry, plus the two fields that adapter didn't need but this one
// does: UUID (this package's ConversationTurn.ID source) and IsSidechain
// (see the isSidechain doc comment on parseLine).
type claudeEntry struct {
	UUID        string `json:"uuid"`
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	SessionID   string `json:"sessionId"`
	Timestamp   string `json:"timestamp"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Origin      *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message json.RawMessage `json:"message"`
}

type claudeMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string (human prompt) or []claudeBlock (assistant, or a user tool_result envelope)
	StopReason string          `json:"stop_reason"`
}

type claudeBlock struct {
	Type     string `json:"type"` // text | thinking | tool_use | tool_result
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
}

// ReadConversation implements runtime.ConversationProvider.
//
// req.TranscriptID is the absolute JSONL file path this adapter's own
// Correlate produces (resolveTranscriptID in correlate.go) — this package
// never resolves a session id to a path on its own inside
// ReadConversation, it only reads the path it is handed. The whole file
// is parsed into an ordered turn slice on every call and paginated the
// same way internal/runtime/fake does (After is a decimal offset into
// that slice, round-tripped through NextCursor): Stage 1 has no
// incremental-tail requirement for ConversationProvider, and matching the
// fake's cursor contract exactly means a caller exercises one pagination
// behavior across both adapters. A live, actively-growing transcript is
// re-read from the start on every call under this design — acceptable for
// Stage 1, recorded as a known cost rather than solved here (see
// docs/adapters/decisions.md, this package's section).
func (r *Runtime) ReadConversation(_ context.Context, req runtime.ConversationReadRequest) (runtime.ConversationBatch, error) {
	all, err := parseTranscript(req.TranscriptID)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}

	offset, err := cursorOffset(req.After)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}
	if offset > len(all) {
		offset = len(all)
	}

	remaining := all[offset:]
	limit := req.Limit
	if limit <= 0 || limit > len(remaining) {
		limit = len(remaining)
	}
	batch := append([]runtime.ConversationTurn(nil), remaining[:limit]...)
	nextOffset := offset + limit

	return runtime.ConversationBatch{
		Turns:      batch,
		NextCursor: strconv.Itoa(nextOffset),
		Complete:   nextOffset >= len(all),
	}, nil
}

func cursorOffset(after string) (int, error) {
	if after == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(after)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("claude runtime: invalid cursor %q", after)
	}
	return n, nil
}

// parseTranscript reads and parses every line of a Claude Code session
// JSONL file into an ordered slice of semantic conversation turns. A
// line that fails to parse as JSON, or whose shape this adapter does not
// recognize, contributes zero turns rather than aborting the read — an
// append-only transcript can have a partially-written trailing line while
// a live session is running (notes/06-claude.md), and one bookkeeping
// entry type this adapter has never seen should not sink the whole read.
// Only os.Open failing is a hard error, since that means the caller handed
// this adapter a TranscriptID it cannot use at all.
func parseTranscript(path string) ([]runtime.ConversationTurn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("claude runtime: opening transcript %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var turns []runtime.ConversationTurn
	for scanner.Scan() {
		turns = append(turns, parseLine(scanner.Bytes())...)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("claude runtime: reading transcript %s: %w", path, err)
	}
	return turns, nil
}

// parseLine turns one JSONL line into zero or more ConversationTurns.
//
// Quirks ported from apps/transcript-tail/adapter_claude.go's Line/
// userEntry/assistantEntry (notes/06-claude.md, notes/16 §1):
//
//   - isSidechain entries are dropped. A subagent's transcript
//     (`<slug>/<session-id>/subagents/agent-<id>.jsonl`) is a physically
//     separate file from the main session's, so a well-formed
//     ReadConversation call should never actually hand this adapter a
//     line with isSidechain: true — this is defense in depth, and the
//     fact that it does the right thing on
//     testdata/claude/subagent-sidechain-truncated.jsonl (zero turns) is
//     asserted directly. §5.3 has no Stage-1 concept of a subagent
//     conversation thread; reading sidechains as their own transcripts is
//     a recorded gap, not attempted here.
//   - isMeta user entries are dropped (bookkeeping, not conversation).
//   - A human-prompt user entry (message.content is a plain string) is
//     dropped when it carries a non-nil origin whose kind isn't "human"
//     — task notifications, and (notes/16 §5) peer-injected turns, whose
//     UserPromptSubmit hook payload is indistinguishable from a human's
//     but whose transcript entry carries the tattling origin.kind:"peer".
//     `-p` mode's origin: null (notes/16 §1, promptSource: "sdk" churn
//     against the 2.1.226 census) and interactive's origin:
//     {kind:"human"} both pass this check unchanged from the original
//     adapter's logic — a nil Origin never trips the "kind != human"
//     branch.
//   - A tool-result user entry (message.content is a block array) yields
//     a turn only for its "text" blocks; "tool_result" blocks are
//     dropped. ConversationTurn's minimal shape (ID, Role, Text, At —
//     docs/adapters/decisions.md on the runtime package) has nowhere to
//     put tool-call bookkeeping; a tool-call/result stream is a
//     documented Stage-1 gap for this provider, same as thinking and
//     tool_use blocks below.
//   - An assistant entry's "text" blocks become assistant turns;
//     "thinking" and "tool_use" blocks are dropped for the same
//     minimal-shape reason. Turn-boundary bookkeeping
//     (system/turn_duration, the end_turn/silence arming the original
//     adapter does for live-tail turn-end detection) has no equivalent
//     here either — Stage 1's ConversationProvider is a semantic-turn
//     reader, not a turn-boundary signal; that belongs to
//     ConditionProvider, which this step does not implement.
//   - Every other entry type (system, mode, permission-mode, attachment
//     — including the new atis-latch, bridge-session, and
//     total_tokens_reminder types notes/16 §1 found at 2.1.240 — ai-title,
//     queue-operation, file-history-*, last-prompt, ...) falls through
//     the type switch and contributes nothing, exactly like the ported
//     adapter's default case. This is why the three new bookkeeping
//     entry types need no dedicated code here: an entry type this
//     adapter has never heard of is safe by construction, not by an
//     enumerated allowlist.
func parseLine(raw []byte) []runtime.ConversationTurn {
	var e claudeEntry
	if json.Unmarshal(raw, &e) != nil {
		return nil
	}
	if e.IsSidechain {
		return nil
	}

	at, _ := time.Parse(time.RFC3339Nano, e.Timestamp) // zero Time on a parse failure; best effort, not fatal.

	switch e.Type {
	case "user":
		return userTurns(e, at)
	case "assistant":
		return assistantTurns(e, at)
	}
	return nil
}

func userTurns(e claudeEntry, at time.Time) []runtime.ConversationTurn {
	if e.IsMeta {
		return nil
	}
	var m claudeMessage
	if json.Unmarshal(e.Message, &m) != nil {
		return nil
	}

	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		if e.Origin != nil && e.Origin.Kind != "human" {
			return nil // task notifications, peer-injected turns, etc. — see parseLine's doc comment.
		}
		return []runtime.ConversationTurn{{ID: e.UUID, Role: "user", Text: text, At: at}}
	}

	var blocks []claudeBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return nil
	}
	var out []runtime.ConversationTurn
	for i, b := range blocks {
		if b.Type != "text" {
			continue // tool_result and any other block kind: dropped, see parseLine's doc comment.
		}
		out = append(out, runtime.ConversationTurn{ID: blockTurnID(e.UUID, i), Role: "user", Text: b.Text, At: at})
	}
	return out
}

func assistantTurns(e claudeEntry, at time.Time) []runtime.ConversationTurn {
	var m claudeMessage
	if json.Unmarshal(e.Message, &m) != nil {
		return nil
	}
	var blocks []claudeBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return nil
	}
	var out []runtime.ConversationTurn
	for i, b := range blocks {
		if b.Type != "text" {
			continue // thinking and tool_use: dropped, see parseLine's doc comment.
		}
		out = append(out, runtime.ConversationTurn{ID: blockTurnID(e.UUID, i), Role: "assistant", Text: b.Text, At: at})
	}
	return out
}

// blockTurnID keeps ConversationTurn.ID unique when one envelope line
// yields more than one turn (a multi-block content array with more than
// one "text" block — not observed in any ported fixture, where Claude
// Code writes one block per line, but the wire format does not forbid
// it). The envelope uuid alone is unique per JSONL line, not per turn.
func blockTurnID(envelopeUUID string, blockIndex int) string {
	if blockIndex == 0 {
		return envelopeUUID
	}
	return fmt.Sprintf("%s#%d", envelopeUUID, blockIndex)
}
