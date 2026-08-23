package launch

import (
	"crypto/rand"
	"fmt"
	"math/big"
	mathrand "math/rand/v2"
)

// Draw is the random evidence one explicit-random selection recorded. §7.2:
// replaying the static inputs reproduces the same eligible assignment set,
// and adding the draw evidence reproduces the same selected assignment. So
// the draw is not a log line — it is the part of the record that makes a
// random resolution replayable at all.
type Draw struct {
	// Source names the entropy source that produced Index.
	Source string `json:"source"`
	// Seed is the source's reproducible seed, when it has one. A
	// cryptographic source has none and leaves this empty.
	Seed string `json:"seed,omitempty"`
	// SetSize is how many complete eligible assignments the draw chose
	// among. Recording it makes the draw meaningful without the candidate
	// table.
	SetSize int `json:"set_size"`
	// Index is the chosen assignment's position in the eligible set, in
	// the resolver's own deterministic enumeration order.
	Index int `json:"index"`
}

// RandomSource draws one index over the eligible complete assignments.
//
// It is an injected dependency with no default: a resolver asked to resolve
// a random-selection preset without one refuses rather than reaching for a
// package-level generator. That keeps every test deterministic and keeps
// the decision path free of hidden entropy.
type RandomSource interface {
	// Draw returns an index in [0, n) together with the evidence that
	// explains it. n is always at least 1. An error here is §6.8's
	// "entropy fails before random selection is recorded" row.
	Draw(n int) (Draw, error)
}

// SeededSource is a reproducible RandomSource: the same seed and the same
// eligible set always draw the same assignment. It is what a test injects,
// and what an installation that wants replayable "random" launches can
// inject too — the seed is recorded on every draw.
type SeededSource struct {
	seed uint64
	rng  *mathrand.Rand
}

// NewSeededSource returns a reproducible source over seed.
func NewSeededSource(seed uint64) *SeededSource {
	return &SeededSource{seed: seed, rng: mathrand.New(mathrand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// Draw implements RandomSource.
func (s *SeededSource) Draw(n int) (Draw, error) {
	if n <= 0 {
		return Draw{}, fmt.Errorf("launch: draw over an empty eligible set")
	}
	return Draw{
		Source:  "seeded-pcg",
		Seed:    fmt.Sprintf("%d", s.seed),
		SetSize: n,
		Index:   s.rng.IntN(n),
	}, nil
}

// CryptoSource is the non-reproducible RandomSource an ordinary
// installation injects: crypto/rand, with the drawn index recorded so the
// resolution still explains its own choice.
type CryptoSource struct{}

// Draw implements RandomSource.
func (CryptoSource) Draw(n int) (Draw, error) {
	if n <= 0 {
		return Draw{}, fmt.Errorf("launch: draw over an empty eligible set")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return Draw{}, fmt.Errorf("launch: drawing entropy: %w", err)
	}
	return Draw{Source: "crypto/rand", SetSize: n, Index: int(v.Int64())}, nil
}
