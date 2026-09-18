// Package opencode implements the deliberately small, read-only OpenCode
// HTTP/SSE observer. It does not contain a prompt or effect path.
package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
)

const (
	// AdapterID is the stable identifier for the OpenCode observer.
	AdapterID = "opencode"
	// PinnedExternalVersion is the supported OpenCode release.
	PinnedExternalVersion = "1.18.31"
	// ExecutableSHA256 identifies the supported OpenCode executable.
	ExecutableSHA256 = "f9dab32248695e9ebd56b16a1921798fd85112cf5a69c7dfd0cabc1e17be4a11"
	// SchemaSHA256 identifies the supported OpenCode API schema.
	SchemaSHA256 = "46db986090aae41846cd6dbe16225a1d883f0bbcb4c48814008d3f6ce140aa5c"
	// ConformanceRecordDigest identifies the adapter's conformance record.
	ConformanceRecordDigest = "duo-opencode-http-sse-herdr-moshi/step11-adapter-plan/2026-09-17"
)

// Binding is the admission identity. Every field is required; in particular,
// cwd, a transcript path, and server_id are intentionally absent.
type Binding struct {
	IntegrationInstanceID string
	ProcessEpoch          string
	Endpoint              string
	SessionID             string
}

// LaunchMetadata records the Duo-owned launch posture. It is not serialized
// into public diagnostics (ports and PIDs are sensitive dynamic data).
type LaunchMetadata struct {
	IntegrationInstanceID string
	ProcessEpoch          string
	Endpoint              string
	ExecutableSHA256      string
	SchemaSHA256          string
	PID                   int
	Port                  int
	SessionID             string
	Flags                 []string
	// OwnershipVerified records that the host's explicit PID/socket ownership
	// seam succeeded. Metadata alone never proves ownership.
	OwnershipVerified bool
}

// Factory admits read-only OpenCode runtimes from host-owned launch facts.
type Factory struct {
	IntegrationInstanceID string
	Binary                string
	Endpoint              string
	Credential            string
	Client                *http.Client
	// DocSource is a test/live seam for retrieving the exact /doc bytes.
	DocSource func(context.Context) ([]byte, error)
	// OwnershipChecker is the host-owned PID/socket assertion. Metadata is
	// never treated as proof when this seam is absent.
	OwnershipChecker func(context.Context, LaunchMetadata) error
	LaunchMetadata   LaunchMetadata
	LaunchSpec       LaunchSpec
	Binding          Binding
}

// Runtime is an admitted, read-only OpenCode observer.
type Runtime struct {
	binding    Binding
	client     *http.Client
	credential string
	// admitted is set only by Factory.New after its independent probe and
	// binding checks. New is retained for source compatibility, but creates
	// an intentionally unadmitted value that cannot read from OpenCode.
	admitted bool
}

func (r *Runtime) ensureAdmitted() error {
	if !r.admitted {
		return fmt.Errorf("opencode: runtime was not admitted by Factory.New")
	}
	return nil
}

// Binding returns the immutable admission identity currently in use.
func (r *Runtime) Binding() Binding { return r.binding }

// CorrelateBinding validates the complete launch tuple. The shared runtime
// claim predates process/endpoint identity, so CorrelateBinding is the
// explicit seam used by launch-aware callers.
func (r *Runtime) CorrelateBinding(b Binding) error {
	if err := r.ensureAdmitted(); err != nil {
		return err
	}
	if err := b.validate(); err != nil {
		return err
	}
	if b.IntegrationInstanceID != r.binding.IntegrationInstanceID || b.ProcessEpoch != r.binding.ProcessEpoch || b.Endpoint != r.binding.Endpoint || b.SessionID != r.binding.SessionID {
		return fmt.Errorf("opencode: runtime binding does not match")
	}
	return nil
}

var (
	_ adapter.Factory[*Runtime]    = Factory{}
	_ runtime.RuntimeCorrelator    = (*Runtime)(nil)
	_ runtime.ConversationProvider = (*Runtime)(nil)
	_ runtime.ConditionProvider    = (*Runtime)(nil)
)

// Descriptor returns the static adapter descriptor.
func (Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID: AdapterID, Role: adapter.RoleRuntime, BuildVersion: "readonly-observer-v1",
		SupportedExternalVersions: []string{PinnedExternalVersion}, ConformanceRecordDigest: ConformanceRecordDigest,
		DiagnosticRedactionPolicy: "credentials-transcript-content-dynamic-paths-pids-ports",
	}
}

// Probe checks the configured OpenCode endpoint without mutating it.
func (f Factory) Probe(ctx context.Context) (adapter.Probe, error) {
	p := adapter.Probe{ProtocolOrFormatIdentity: "opencode-http-sse/v1", ConnectionState: "absent", Compatibility: adapter.CompatibilityUnavailable}
	if !loopbackEndpoint(f.Endpoint) {
		return p, nil
	}
	bin := f.Binary
	if bin == "" {
		bin = "opencode"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return p, nil
	}
	p.ConnectionState = "unreadable"
	digest, err := fileSHA256(path)
	if err != nil {
		return p, nil
	}
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return p, nil
	}
	p.DetectedVersion = versionFrom(string(out))
	p.ConnectionState = "installed"
	if f.Credential == "" {
		return p, nil
	}
	var doc []byte
	// Admission probes both authenticated health and the pinned schema. A
	// schema hash alone is not evidence that the server accepts our secret.
	if _, err := f.get(ctx, "/global/health"); err != nil {
		return p, nil
	}
	if f.DocSource != nil {
		doc, err = f.DocSource(ctx)
	} else {
		doc, err = f.get(ctx, "/doc")
	}
	if err != nil {
		return p, nil
	}
	sd := sha256.Sum256(doc)
	p.FixtureOrSchemaDigest = hex.EncodeToString(sd[:])
	if digest == ExecutableSHA256 && p.DetectedVersion == PinnedExternalVersion && p.FixtureOrSchemaDigest == SchemaSHA256 {
		p.Compatibility = adapter.CompatibilitySupported
	} else {
		p.Compatibility = adapter.CompatibilityUnverified
	}
	return p, nil
}

// New admits a runtime only after the probe and launch identities agree.
func (f Factory) New(ctx context.Context, p adapter.Probe) (*Runtime, error) {
	if p.Compatibility != adapter.CompatibilitySupported || p.ProtocolOrFormatIdentity != "opencode-http-sse/v1" || p.DetectedVersion != PinnedExternalVersion || p.FixtureOrSchemaDigest != SchemaSHA256 {
		return nil, fmt.Errorf("opencode: refusing unavailable or incompatible runtime")
	}
	// Probe values are public structs and therefore forgeable. Re-probe the
	// factory-owned binary and document source at the admission boundary.
	verified, err := f.Probe(ctx)
	if err != nil || verified != p || verified.Compatibility != adapter.CompatibilitySupported {
		return nil, fmt.Errorf("opencode: admission probe no longer matches factory")
	}
	if err := ValidateLaunchMetadata(f.LaunchMetadata, f.LaunchSpec); err != nil {
		return nil, err
	}
	if f.OwnershipChecker == nil {
		return nil, fmt.Errorf("opencode: launch ownership assertion required")
	}
	if err := f.OwnershipChecker(ctx, f.LaunchMetadata); err != nil {
		return nil, fmt.Errorf("opencode: launch ownership assertion failed")
	}
	if f.Endpoint == "" || !loopbackEndpoint(f.Endpoint) {
		return nil, fmt.Errorf("opencode: refusing unbound endpoint")
	}
	if f.Credential == "" {
		return nil, fmt.Errorf("opencode: credentials required")
	}
	if err := f.Binding.validate(); err != nil {
		return nil, fmt.Errorf("opencode: refusing unbound runtime")
	}
	if err := validateLaunchBindingIdentity(f.LaunchMetadata, f.Binding, f.IntegrationInstanceID, f.Endpoint); err != nil {
		return nil, fmt.Errorf("opencode: runtime endpoint does not match admission")
	}
	return &Runtime{binding: f.Binding, client: clientOrDefault(f.Client), credential: f.Credential, admitted: true}, nil
}

func validateLaunchBindingIdentity(m LaunchMetadata, b Binding, instance, endpoint string) error {
	if instance == "" || m.IntegrationInstanceID != instance || b.IntegrationInstanceID != instance || m.Endpoint != endpoint || b.Endpoint != endpoint || m.ProcessEpoch != b.ProcessEpoch || m.SessionID != b.SessionID {
		return fmt.Errorf("opencode: launch and binding identities do not match")
	}
	return nil
}

// New is an unadmitted constructor retained for compatibility with old
// callers. It is not an admission path: the resulting Runtime cannot perform
// reads. Use Factory.New after Probe for an admitted runtime.
func New(binding Binding, client *http.Client, credential string) (*Runtime, error) {
	if err := binding.validate(); err != nil {
		return nil, err
	}
	if credential == "" {
		return nil, fmt.Errorf("opencode: credentials required")
	}
	return &Runtime{binding: binding, client: clientOrDefault(client), credential: credential}, nil
}

// Correlate proves a claim against this runtime's admitted binding.
func (r *Runtime) Correlate(_ context.Context, c runtime.RuntimeClaim) (runtime.RuntimeCorrelationEvidence, error) {
	// RuntimeClaim has no process-epoch field. Epoch provenance is therefore
	// established exactly once by Factory.New and cannot be supplied or
	// changed through Correlate; session and integration identity must still
	// match this immutable admitted binding.
	if err := r.ensureAdmitted(); err != nil {
		return runtime.RuntimeCorrelationEvidence{}, err
	}
	if c.IntegrationInstanceID != r.binding.IntegrationInstanceID {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf("opencode: integration instance does not match")
	}
	if c.ExternalAgentSessionID != r.binding.SessionID {
		return runtime.RuntimeCorrelationEvidence{}, nil
	}
	return runtime.RuntimeCorrelationEvidence{ExternalAgentSessionID: r.binding.SessionID, Bound: true, Confidence: "opencode-bound-epoch-loopback"}, nil
}

func (b Binding) validate() error {
	if b.IntegrationInstanceID == "" || strings.IndexFunc(b.IntegrationInstanceID, func(r rune) bool { return r < 0x20 }) >= 0 || b.ProcessEpoch == "" || strings.IndexFunc(b.ProcessEpoch, func(r rune) bool { return r < 0x20 }) >= 0 || !loopbackEndpoint(b.Endpoint) || !sessionIDRE.MatchString(b.SessionID) {
		return fmt.Errorf("opencode: incomplete runtime binding")
	}
	return nil
}

func loopbackEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	port, portErr := strconv.Atoi(u.Port())
	return err == nil && u.Scheme == "http" && u.Hostname() == "127.0.0.1" && portErr == nil && port >= 1 && port <= 65535 && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

var sessionIDRE = regexp.MustCompile(`^ses_[a-zA-Z0-9_-]{1,127}$`)

func (b Binding) sessionPath(suffix string) string {
	return "/session/" + url.PathEscape(b.SessionID) + suffix
}

func clientOrDefault(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func versionFrom(s string) string {
	for _, line := range strings.Fields(s) {
		if strings.Count(line, ".") >= 2 {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (f Factory) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(f.Endpoint, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	if f.Credential == "" {
		return nil, fmt.Errorf("opencode: credentials required")
	}
	req.Header.Set("Authorization", f.Credential)
	resp, err := clientOrDefault(f.Client).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencode: read rejected")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil || len(b) > 4<<20 {
		return nil, fmt.Errorf("opencode: schema response too large")
	}
	return b, nil
}
