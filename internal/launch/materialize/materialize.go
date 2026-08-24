package materialize

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
)

// Options is one materialization's inputs. Every dependency that touches
// the outside world is injected: the environment, the working directory,
// the clock, and instance discovery. Nothing here reaches for a package
// global, which is what lets a test drive the whole ladder without a
// process environment or a store.
type Options struct {
	// WorkspaceFlag is --workspace, "" when it was not given.
	WorkspaceFlag string
	// HostFlag is the raw --host value, "" when it was not given.
	HostFlag string
	// RequestedPreset names the preset in a failure's safe details.
	RequestedPreset string

	// Policy is the loaded document's session_hosts block: host-kind
	// policy only, never an instance.
	Policy config.SessionHostPolicy

	// Correlations is the workspace↔host read model. A nil source means
	// no correlation is available, and the rung reports that rather than
	// panicking — `duo doctor` on a machine with no store is a real case.
	Correlations CorrelationSource
	// Providers is the standing-provider read model M2 snapshots. Nil
	// yields an empty snapshot, which by the default-enabled rule means
	// no provider is disabled.
	Providers ProviderSource
	// Discovery enumerates instances of a kind for the policy-default
	// rung and for the kind-only form of --host. Nil means this build can
	// discover nothing, which the trail says out loud.
	Discovery InstanceDiscovery

	// AmbientSources is the ambient signature table. Nil means
	// DefaultAmbientSources.
	AmbientSources []AmbientSource
	// LookupEnv reads one environment variable. Nil means os.LookupEnv.
	// The ambient rung calls it exactly once per distinct variable name,
	// which is the property that makes a capture evidence rather than a
	// re-read that could disagree with itself.
	LookupEnv func(string) (string, bool)
	// Getwd resolves the working directory when --workspace is absent.
	// Nil means os.Getwd.
	Getwd func() (string, error)
	// Now stamps the ambient captures. Nil means time.Now.
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.AmbientSources == nil {
		o.AmbientSources = DefaultAmbientSources
	}
	if o.LookupEnv == nil {
		o.LookupEnv = os.LookupEnv
	}
	if o.Getwd == nil {
		o.Getwd = os.Getwd
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// Materialize runs M1 then M2 and returns the immutable evidence bundle the
// resolver reads.
//
// M1 resolves the workspace (--workspace, else the working directory) and
// walks the fixed ranking. The three evidence rungs — explicit flag,
// workspace correlation, ambient environment — are always evaluated, even
// once one of them has won, because a beaten rung is exactly the evidence
// that makes a wrong binding visible. The policy-default rung is evaluated
// only when none of them yielded, because it is the one rung that costs
// I/O. M2 then snapshots the standing provider facts.
//
// The one exception to "evaluate every evidence rung" is an explicit flag
// that cannot be honored: an operator who named a host and did not get it
// must be told, never quietly given the correlation they were overriding.
// That stops the ladder, and the remaining rungs record consulted = false.
//
// Materialize never checks that the deduced instance is reachable (I-3) and
// never writes anything.
func Materialize(ctx context.Context, opts Options) (Result, error) {
	opts = opts.withDefaults()

	flag, err := ParseHostFlag(opts.HostFlag)
	if err != nil {
		return Result{}, err
	}

	path := opts.WorkspaceFlag
	if path == "" {
		wd, err := opts.Getwd()
		if err != nil {
			return Result{}, duoerr.New("internal.cwd_unresolved",
				fmt.Sprintf("resolving the working directory: %v", err))
		}
		path = wd
	}

	m := &pass{opts: opts, flag: flag, path: path}
	if opts.Correlations != nil {
		if w, ok := opts.Correlations.WorkspaceForRoot(path); ok {
			m.workspace = w.ID
		}
	}
	return m.run(ctx)
}

// pass is one materialization in progress. It exists so the rung methods
// can share the captured evidence without threading six values through
// every signature.
type pass struct {
	opts      Options
	flag      HostFlag
	path      string
	workspace domain.WorkspaceID

	// captures holds the ambient variables read at the ambient rung, in
	// read order. It is read once and never re-read.
	captures []AmbientCapture
	// correlationFactID and correlationFingerprint hold what the
	// correlation rung saw, whether or not it won.
	correlationFactID      domain.FactID
	correlationFingerprint domain.HostFingerprint
	hasCorrelation         bool
}

// outcome is one rung's result: what the trail records, plus the host it
// produced, plus whether the ladder must stop here.
type outcome struct {
	rung DeductionRung
	host DeducedHost
	stop bool
}

func (m *pass) run(ctx context.Context) (Result, error) {
	outcomes := make([]outcome, 0, len(Rungs))

	flagOut, err := m.explicitFlagRung(ctx)
	if err != nil {
		return Result{}, err
	}
	outcomes = append(outcomes, flagOut)

	if !flagOut.stop {
		outcomes = append(outcomes, m.correlationRung())
		outcomes = append(outcomes, m.ambientRung())
	}

	// The policy-default rung is the only one that costs discovery I/O, so
	// it is asked only when no evidence rung answered.
	if !flagOut.stop && !anyYielded(outcomes) {
		defaultOut, err := m.policyDefaultRung(ctx)
		if err != nil {
			return Result{}, err
		}
		outcomes = append(outcomes, defaultOut)
	}
	outcomes = fillSkipped(outcomes, flagOut.stop)

	result := m.assemble(outcomes)
	if !result.host.Present() {
		return Result{}, hostUnresolved(result, m.opts.RequestedPreset, m.enabledKinds())
	}
	return result, nil
}

// assemble turns the rung outcomes into the immutable Result: the winner is
// the highest-ranked rung that yielded, everything else that yielded is
// outranked evidence, and M2's snapshot completes the bundle.
func (m *pass) assemble(outcomes []outcome) Result {
	winner := -1
	for i, o := range outcomes {
		if o.rung.YieldedHost {
			winner = i
			break
		}
	}

	var host DeducedHost
	if winner >= 0 {
		host = outcomes[winner].host
		host.Workspace = m.workspace
	}

	var outranked []OutrankedEvidence
	for i, o := range outcomes {
		if i == winner {
			continue
		}
		entry := OutrankedEvidence{Source: o.rung.Source}
		carries := false
		if o.rung.YieldedHost {
			entry.Kind = o.rung.Kind
			entry.Instance = o.rung.Instance
			carries = true
		}
		if o.rung.Source == domain.HostSourceWorkspaceCorrelation && m.hasCorrelation {
			entry.FactID = m.correlationFactID
			carries = true
		}
		if o.rung.Source == domain.HostSourceAmbientEnv && len(m.captures) > 0 {
			entry.Captures = cloneCaptures(m.captures)
			carries = true
		}
		if !carries {
			continue
		}
		if winner >= 0 {
			entry.Detail = "outranked by " + string(outcomes[winner].rung.Source)
		} else {
			entry.Detail = o.rung.Detail
		}
		outranked = append(outranked, entry)
	}

	trail := make([]DeductionRung, 0, len(outcomes))
	for _, o := range outcomes {
		trail = append(trail, o.rung)
	}

	return Result{
		workspacePath: m.path,
		workspaceID:   m.workspace,
		host:          host,
		outranked:     outranked,
		trail:         trail,
		bundle: EvidenceBundle{
			correlationFactID:      m.correlationFactID,
			correlationFingerprint: m.correlationFingerprint,
			hasCorrelation:         m.hasCorrelation,
			ambient:                cloneCaptures(m.captures),
			providers:              m.snapshotProviders(),
		},
	}
}

// snapshotProviders is M2: the standing provider facts, copied out of the
// read model once, so the resolver and the record it explains cite the same
// facts even if the kernel moves on underneath them.
func (m *pass) snapshotProviders() map[string]domain.ProviderStanding {
	if m.opts.Providers == nil {
		return map[string]domain.ProviderStanding{}
	}
	src := m.opts.Providers.StandingProviderFacts()
	out := make(map[string]domain.ProviderStanding, len(src))
	for name, st := range src {
		out[name] = st
	}
	return out
}

// --- the four rungs, in rank order ---------------------------------------

// explicitFlagRung is rung 1. It has no `deduce` key: installed policy
// never switches off a host the operator typed.
//
// A flag that names `<kind>:<instance>` yields outright. A flag that names
// only a kind is resolved through discovery, and every ambiguity there —
// no discoverer, no instance, several instances — stops the ladder rather
// than falling through, because falling through would hand the operator
// the very host they were overriding.
func (m *pass) explicitFlagRung(ctx context.Context) (outcome, error) {
	rung := DeductionRung{Source: domain.HostSourceExplicitFlag}
	if !m.flag.Present() {
		rung.Detail = "no --host flag supplied"
		return outcome{rung: rung}, nil
	}
	rung.Consulted = true
	rung.Kind = m.flag.Kind

	if m.flag.Complete() {
		rung.YieldedHost = true
		rung.Instance = m.flag.Instance
		rung.Detail = "--host named " + m.flag.String()
		return outcome{rung: rung, host: DeducedHost{
			Kind:     m.flag.Kind,
			Instance: m.flag.Instance,
			Source:   domain.HostSourceExplicitFlag,
		}}, nil
	}

	inst, detail, err := m.discoverOne(ctx, m.flag.Kind)
	if err != nil {
		return outcome{}, err
	}
	if inst == nil {
		rung.Detail = detail
		return outcome{rung: rung, stop: true}, nil
	}
	rung.YieldedHost = true
	rung.Instance = inst.Locator
	rung.Detail = detail
	return outcome{rung: rung, host: DeducedHost{
		Kind:       m.flag.Kind,
		Instance:   inst.Locator,
		InstanceID: inst.InstanceID,
		Source:     domain.HostSourceExplicitFlag,
	}}, nil
}

// correlationRung is rung 2: the persisted workspace↔host binding. It is
// intent — somebody bound this workspace to this instance on purpose — which
// is why it outranks the ambient environment, which is accident.
//
// It reads the correlation even when a higher rung already won, so a stale
// binding shows up as outranked evidence next to the rebind pointer.
func (m *pass) correlationRung() outcome {
	rung := DeductionRung{Source: domain.HostSourceWorkspaceCorrelation}
	if !m.deduceEnabled(DeduceWorkspace) {
		rung.Detail = disabledDetail(DeduceWorkspace)
		return outcome{rung: rung}
	}
	rung.Consulted = true
	if m.opts.Correlations == nil {
		rung.Detail = "no workspace-host correlation read model is available"
		return outcome{rung: rung}
	}
	if m.workspace == "" {
		rung.Detail = "the workspace path is not enrolled, so it has no correlation"
		return outcome{rung: rung}
	}
	c, ok := m.opts.Correlations.HostCorrelation(m.workspace)
	if !ok {
		rung.Detail = "no persisted workspace-host correlation for " + string(m.workspace)
		return outcome{rung: rung}
	}

	m.hasCorrelation = true
	m.correlationFactID = c.FactID
	m.correlationFingerprint = c.Binding.Fingerprint

	rung.YieldedHost = true
	rung.Kind = c.Binding.Kind
	rung.Instance = c.Binding.Instance
	rung.Detail = "workspace " + string(m.workspace) + " is bound to " + c.Binding.Locator()
	return outcome{rung: rung, host: DeducedHost{
		Kind:        c.Binding.Kind,
		Instance:    c.Binding.Instance,
		InstanceID:  c.Binding.InstanceID,
		Source:      domain.HostSourceWorkspaceCorrelation,
		Fingerprint: c.Binding.Fingerprint,
	}}
}

// ambientRung is rung 3: the host variables published into the pane this
// Duo is running in. Accident, not intent — notes/19 §0's precedence
// footgun is why it sits below the correlation.
//
// This is the only place the environment is read, and each distinct
// variable is read exactly once per materialization. Everything read is
// captured, including values from a source that did not yield and values
// the winner outranked: what was true at materialization is evidence
// whether or not it decided anything.
func (m *pass) ambientRung() outcome {
	rung := DeductionRung{Source: domain.HostSourceAmbientEnv}
	if !m.deduceEnabled(DeduceEnv) {
		rung.Detail = disabledDetail(DeduceEnv)
		return outcome{rung: rung}
	}
	rung.Consulted = true

	values := map[string]string{}
	stamp := m.opts.Now().UTC().Format(CaptureTimeLayout)
	for _, name := range AmbientVariableNames(m.opts.AmbientSources) {
		value, ok := m.opts.LookupEnv(name)
		if !ok {
			continue
		}
		values[name] = value
		m.captures = append(m.captures, AmbientCapture{Name: name, Value: value, CapturedAt: stamp})
	}

	var unset []string
	for _, s := range m.opts.AmbientSources {
		locator, ok := values[s.LocatorVar]
		if !ok || locator == "" {
			unset = append(unset, s.LocatorVar)
			continue
		}
		instanceID := ""
		if s.InstanceVar != "" {
			if session, ok := values[s.InstanceVar]; ok && session != "" {
				instanceID = s.InstanceIDPrefix + session
			}
		}
		rung.YieldedHost = true
		rung.Kind = s.Kind
		rung.Instance = locator
		rung.Detail = s.LocatorVar + " names " + s.Kind + ":" + locator
		return outcome{rung: rung, host: DeducedHost{
			Kind:       s.Kind,
			Instance:   locator,
			InstanceID: instanceID,
			Source:     domain.HostSourceAmbientEnv,
		}}
	}

	rung.Detail = strings.Join(unset, ", ") + " not set"
	if len(unset) == 0 {
		rung.Detail = "no ambient host variables are known to this build"
	}
	return outcome{rung: rung}
}

// policyDefaultRung is rung 4: the first enabled kind in
// `session_hosts.prefer`, with the instance from discovery.
//
// It never falls through to the second-preferred kind when the first yields
// nothing, and it never picks when discovery finds several: a default that
// silently substitutes a kind, or silently chooses among instances, is a
// wrong launch nobody can see (workplan Risk 5). Both come back as no host,
// with the trail saying which it was.
func (m *pass) policyDefaultRung(ctx context.Context) (outcome, error) {
	rung := DeductionRung{Source: domain.HostSourcePolicyDefault}
	if !m.deduceEnabled(DeduceDefault) {
		rung.Detail = disabledDetail(DeduceDefault)
		return outcome{rung: rung}, nil
	}
	rung.Consulted = true

	enabled := m.enabledKinds()
	if len(enabled) == 0 {
		rung.Detail = "no enabled kind in session_hosts.prefer"
		return outcome{rung: rung}, nil
	}
	kind := enabled[0]
	rung.Kind = kind

	inst, detail, err := m.discoverOne(ctx, kind)
	if err != nil {
		return outcome{}, err
	}
	rung.Detail = detail
	if inst == nil {
		return outcome{rung: rung}, nil
	}
	rung.YieldedHost = true
	rung.Instance = inst.Locator
	return outcome{rung: rung, host: DeducedHost{
		Kind:       kind,
		Instance:   inst.Locator,
		InstanceID: inst.InstanceID,
		Source:     domain.HostSourcePolicyDefault,
	}}, nil
}

// discoverOne enumerates one kind's instances and insists on exactly one.
// It returns (nil, detail, nil) when it cannot, never a pick.
//
// A discoverer that fails outright is a different thing from one that found
// nothing — "I could not look" is not "there is none" — so its error is
// returned rather than folded into the trail as an absence.
func (m *pass) discoverOne(ctx context.Context, kind string) (*Instance, string, error) {
	if m.opts.Discovery == nil {
		return nil, fmt.Sprintf(
			"no host-instance discovery is available in this build; name the instance as --host %s:<instance>", kind), nil
	}
	found, err := m.opts.Discovery.DiscoverInstances(ctx, kind)
	if err != nil {
		return nil, "", fmt.Errorf("materialize: discovering %s instances: %w", kind, err)
	}
	switch len(found) {
	case 0:
		return nil, fmt.Sprintf(
			"discovery found no %s instance; name one as --host %s:<instance>", kind, kind), nil
	case 1:
		return &found[0], fmt.Sprintf("discovery found one %s instance, %s", kind, found[0].Locator), nil
	default:
		locators := make([]string, 0, len(found))
		for _, f := range found {
			locators = append(locators, f.Locator)
		}
		sort.Strings(locators)
		return nil, fmt.Sprintf(
			"discovery found %d %s instances (%s); name one as --host %s:<instance>",
			len(found), kind, strings.Join(locators, ", "), kind), nil
	}
}

// --- policy helpers -------------------------------------------------------

// enabledKinds returns the `prefer` list with the disabled kinds removed,
// in preference order. An absent kinds stanza, and an absent `enabled` flag
// inside one, both mean enabled — the v3 schema's stated default, applied
// here because internal/config deliberately preserves absent as distinct
// from false and leaves the default to the reader.
func (m *pass) enabledKinds() []string {
	out := make([]string, 0, len(m.opts.Policy.Prefer))
	for _, kind := range m.opts.Policy.Prefer {
		if k, ok := m.opts.Policy.Kinds[kind]; ok && k.Enabled != nil && !*k.Enabled {
			continue
		}
		out = append(out, kind)
	}
	return out
}

// deduceEnabled reports whether one `session_hosts.deduce` source is on.
// Absent means enabled, same default as the kinds stanza. The list only
// ever switches a rung on or off; it never reorders the ranking, which is
// fixed in Rungs.
func (m *pass) deduceEnabled(source string) bool {
	d, ok := m.opts.Policy.Deduce[source]
	if !ok || d.Enabled == nil {
		return true
	}
	return *d.Enabled
}

func disabledDetail(source string) string {
	return fmt.Sprintf("deduction source %q is disabled by session_hosts.deduce", source)
}

// anyYielded reports whether any rung so far produced a host.
func anyYielded(outcomes []outcome) bool {
	for _, o := range outcomes {
		if o.rung.YieldedHost {
			return true
		}
	}
	return false
}

// fillSkipped appends the rungs the ladder never reached, so the trail
// always carries all four in rank order. stopped distinguishes the two
// reasons a rung goes unreached, which is the difference between "an
// earlier rung already answered" and "an explicit flag failed and nothing
// below it was allowed to answer".
func fillSkipped(outcomes []outcome, stopped bool) []outcome {
	reached := map[domain.HostSource]bool{}
	for _, o := range outcomes {
		reached[o.rung.Source] = true
	}
	detail := "not consulted: a higher rung deduced the host"
	if stopped {
		detail = "not consulted: the explicit --host flag could not be resolved"
	}
	for _, source := range Rungs {
		if reached[source] {
			continue
		}
		outcomes = append(outcomes, outcome{rung: DeductionRung{Source: source, Detail: detail}})
	}
	sortByRank(outcomes)
	return outcomes
}

// sortByRank puts the outcomes back in rank order after the skipped rungs
// were appended.
func sortByRank(outcomes []outcome) {
	rank := map[domain.HostSource]int{}
	for i, s := range Rungs {
		rank[s] = i
	}
	sort.SliceStable(outcomes, func(i, j int) bool {
		return rank[outcomes[i].rung.Source] < rank[outcomes[j].rung.Source]
	})
}
