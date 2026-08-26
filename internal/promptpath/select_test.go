package promptpath

import "testing"

func TestSelectPrefersRuntimeWhenQualityMet(t *testing.T) {
	runtime := Offer{Kind: KindRuntime, Quality: QualityExact, Realization: RealizationNative}
	host := Offer{Kind: KindHost, Quality: QualityExact, Realization: RealizationNative}

	got, err := (Selector{}).Select(&runtime, &host, QualityExact)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Kind != KindRuntime {
		t.Fatalf("kind = %s, want runtime", got.Kind)
	}
}

func TestSelectFallsThroughToHostWhenRuntimeMissesQuality(t *testing.T) {
	runtime := Offer{Kind: KindRuntime, Quality: QualityHeuristic, Realization: RealizationNative}
	host := Offer{Kind: KindHost, Quality: QualityDegraded, Realization: RealizationAdapted}

	got, err := (Selector{}).Select(&runtime, &host, QualityDegraded)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Kind != KindHost {
		t.Fatalf("kind = %s, want host", got.Kind)
	}
}

func TestSelectHostWhenRuntimeAbsent(t *testing.T) {
	host := Offer{Kind: KindHost, Quality: QualityDegraded, Realization: RealizationNative}

	got, err := (Selector{}).Select(nil, &host, QualityHeuristic)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Kind != KindHost {
		t.Fatalf("kind = %s, want host (Pi has no runtime path)", got.Kind)
	}
}

func TestSelectNoEligiblePath(t *testing.T) {
	runtime := Offer{Kind: KindRuntime, Quality: QualityHeuristic, Realization: RealizationNative}
	host := Offer{Kind: KindHost, Quality: QualityHeuristic, Realization: RealizationSynthesized}

	_, err := (Selector{}).Select(&runtime, &host, QualityExact)
	if err != ErrNoEligiblePath {
		t.Fatalf("err = %v, want ErrNoEligiblePath", err)
	}
}
