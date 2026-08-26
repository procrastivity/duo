// Package promptpath is the prompt.deliver path-selection seam: prefer a
// RuntimePromptProvider offer that meets the caller's minimum quality;
// otherwise a HostPromptProvider offer. This package names the rule and
// ranks already-collected offers. It does not call adapters, Herdr, or a
// messaging socket.
//
// Claude and Pi both fill RuntimePromptProvider. Prefer-runtime when the
// offer meets minimum quality is unchanged; an adapter that does not
// implement the interface, or whose PromptPath errors, still falls through
// to the host offer.
package promptpath

import "errors"

// Kind is which adapter role an offer came from.
type Kind string

// Closed path kinds, matching domain.PromptPathKind.
const (
	KindRuntime Kind = "runtime"
	KindHost    Kind = "host"
)

// Quality is domain-language operation quality: exact, degraded, or
// heuristic.
type Quality string

// Closed quality values.
const (
	QualityExact     Quality = "exact"
	QualityDegraded  Quality = "degraded"
	QualityHeuristic Quality = "heuristic"
)

// Realization is domain-language operation realization.
type Realization string

// Closed realization values.
const (
	RealizationNative      Realization = "native"
	RealizationAdapted     Realization = "adapted"
	RealizationSynthesized Realization = "synthesized"
)

// Offer is one collected prompt path. Adapters return their own
// PromptPathCandidate; the composer copies quality and realization here
// before asking Selector to choose.
type Offer struct {
	Kind        Kind
	Quality     Quality
	Realization Realization
}

// ErrNoEligiblePath reports that neither offer meets minimum quality.
var ErrNoEligiblePath = errors.New("promptpath: no eligible prompt path meets minimum quality")

// Selector applies the slice rule: prefer runtime when that offer meets
// min quality; else host. Empty min accepts any declared quality. Nil
// offers are skipped (the adapter did not offer a runtime path).
type Selector struct{}

// Select chooses one offer. It does not invoke adapters.
func (Selector) Select(runtime *Offer, host *Offer, minimum Quality) (Offer, error) {
	if runtime != nil && runtime.Kind == KindRuntime && meets(runtime.Quality, minimum) {
		return *runtime, nil
	}
	if host != nil && host.Kind == KindHost && meets(host.Quality, minimum) {
		return *host, nil
	}
	return Offer{}, ErrNoEligiblePath
}

func meets(got, minimum Quality) bool {
	if minimum == "" {
		return rank(got) > 0
	}
	return rank(got) >= rank(minimum)
}

func rank(q Quality) int {
	switch q {
	case QualityHeuristic:
		return 1
	case QualityDegraded:
		return 2
	case QualityExact:
		return 3
	default:
		return 0
	}
}
