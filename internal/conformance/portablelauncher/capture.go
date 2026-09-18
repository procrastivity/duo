package portablelauncher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)authorization["']?\s*[:=]\s*["']?\s*(bearer|basic)\s+[a-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret)["']?\s*[:=]`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`),
	regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----`),
}

var forbiddenEvidenceTerms = []string{
	"terminal_snapshot", "terminal-paste", "terminal_paste", "send_keys", "pane.send_text", "pane.send_keys",
	"raw_transcript", "chain_of_thought", "environment_dump",
}

// Scrub checks bytes before they can enter a content-addressed capture. Paths
// must already have been rewritten to stable tokens.
func Scrub(data []byte) []string {
	var findings []string
	for _, pattern := range secretPatterns {
		if pattern.Match(data) {
			findings = append(findings, "secret-shaped content")
			break
		}
	}
	lower := strings.ToLower(string(data))
	for _, term := range forbiddenEvidenceTerms {
		if strings.Contains(lower, term) {
			findings = append(findings, "forbidden evidence term: "+term)
		}
	}
	for _, marker := range []string{"/home/", "/users/", "/tmp/", "/var/tmp/", "/private/var/", `c:\\users\\`} {
		if strings.Contains(lower, marker) {
			findings = append(findings, "absolute user/run path was not rewritten")
			break
		}
	}
	sort.Strings(findings)
	return findings
}

// Capture stores scrubbed, content-addressed JSON evidence for one suite run.
type Capture struct {
	entries []BlobEntry
	blobs   map[string][]byte
}

// NewCapture creates an empty evidence capture.
func NewCapture() *Capture { return &Capture{blobs: map[string][]byte{}} }

// AddJSON scrubs and adds one JSON evidence value, returning its blob reference.
func (c *Capture) AddJSON(producer string, value any) (string, error) {
	if !oneOf(producer, "common_collector", "controller", "duo", "launcher") {
		return "", fmt.Errorf("unknown evidence producer %q", producer)
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal evidence: %w", err)
	}
	b = append(b, '\n')
	if findings := Scrub(b); len(findings) != 0 {
		return "", fmt.Errorf("evidence scrub failed: %s", strings.Join(findings, "; "))
	}
	ref := "blob:" + Digest(b)
	if old, ok := c.blobs[ref]; ok && !bytes.Equal(old, b) {
		return "", fmt.Errorf("content-address collision at %s", ref)
	}
	if _, exists := c.blobs[ref]; !exists {
		c.blobs[ref] = append([]byte(nil), b...)
		c.entries = append(c.entries, BlobEntry{
			Reference: ref, MediaType: "application/json", Bytes: int64(len(b)),
			SHA256: strings.TrimPrefix(ref, "blob:"), ScrubStatus: "pass", Producer: producer,
		})
	}
	return ref, nil
}

// Index returns a deterministic evidence index for the captured blobs.
func (c *Capture) Index() EvidenceIndex {
	entries := append([]BlobEntry(nil), c.entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Reference < entries[j].Reference })
	return EvidenceIndex{Schema: IndexSchema, Policy: ScrubPolicy, Blobs: entries}
}

// Blob returns a copy of the content-addressed evidence bytes.
func (c *Capture) Blob(ref string) ([]byte, bool) {
	b, ok := c.blobs[ref]
	return append([]byte(nil), b...), ok
}
