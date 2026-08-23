package launch

import "fmt"

// exhausted builds §6.8's launch.constraints_exhausted failure: a require
// or a cross-leaf relation left no complete assignment, and every avoid
// that could relent already has.
//
// It is class invalid — the caller can correct it by changing constraints
// or preset — and it deliberately does not borrow the Locked `unsupported`
// meaning, which is about what this build can do at all.
func (r *Resolver) exhausted(req Request, p *plan, constraints Constraints, rejections []RelationRejection) *Error {
	return newError(CodeConstraintsExhausted,
		exhaustionMessage(constraints, rejections),
		Retry{Safe: true, Action: "change_constraints_or_preset"},
		exhaustionDetails{
			RequestedPreset: req.Preset,
			Constraints:     constraints.wire(),
			Candidates:      wireCandidates(p),
			Survivors: Survivors{
				BeforeConstraints:     poolLocators(p, func(l *leafPlan) []*candidate { return l.beforeConstraints }),
				AfterRequire:          poolLocators(p, func(l *leafPlan) []*candidate { return l.afterRequire }),
				AfterAvoid:            poolLocators(p, func(l *leafPlan) []*candidate { return l.afterAvoid }),
				AfterAvoidRestoration: restorationSurvivors(p),
			},
			// Exhaustion is exactly the case where nothing survived.
			// Recording the zero explicitly is §6.8's required detail, and
			// it distinguishes "the enumeration ran and found none" from
			// "the enumeration never ran".
			CompleteAssignmentsSurvived: 0,
		})
}

// exhaustionMessage names what did the eliminating. A require is named
// first because it is the non-relenting verb: when one is present it is
// always part of the reason, and a caller reading the message needs the
// predicate it must change.
func exhaustionMessage(constraints Constraints, rejections []RelationRejection) string {
	if len(constraints.Require) > 0 {
		return fmt.Sprintf("Require %s left no complete assignment.", joinPredicates(constraints.Require))
	}
	if len(rejections) > 0 {
		return fmt.Sprintf("Relation %s left no complete assignment.", rejections[0].Kind)
	}
	return "No complete assignment survived the requested constraints."
}

// noEligibleCandidate builds §6.8's launch.no_eligible_candidate failure:
// installed evidence or an enabled-host declaration, not the caller's
// constraints, left a leaf with nothing to assign.
//
// It is class unavailable and its safe detail is candidate availability
// outcomes plus the consulted record digests — never the adapter internals
// or paths behind them, which stay with diagnostics.read.
func (r *Resolver) noEligibleCandidate(req Request, p *plan, digests []string, empty *leafPlan) *Error {
	if digests == nil {
		digests = []string{}
	}
	return newError(CodeNoEligibleCandidate,
		fmt.Sprintf("No candidate declared for leaf %q is supported by installed evidence.", empty.name),
		Retry{Safe: true, Action: "install_or_enable_a_supported_candidate"},
		eligibilityDetails{
			RequestedPreset:        req.Preset,
			Leaf:                   empty.name,
			Candidates:             wireCandidates(p),
			ConsultedRecordDigests: digests,
		})
}

// eligibilityDetails is launch.no_eligible_candidate's safe detail.
type eligibilityDetails struct {
	RequestedPreset        string          `json:"requested_preset"`
	Leaf                   string          `json:"leaf"`
	Candidates             []wireCandidate `json:"candidates"`
	ConsultedRecordDigests []string        `json:"consulted_record_digests"`
}

// wireCandidates projects every declared candidate, in leaf order then
// array order, with the stable reason for any that were eliminated. §6.8
// requires "every safe candidate" on an exhaustion, so this lists the whole
// declaration and not just the survivors.
func wireCandidates(p *plan) []wireCandidate {
	out := []wireCandidate{}
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			out = append(out, wireCandidate{
				Leaf:               leaf.name,
				DeclarationLocator: c.locator(),
				AgentRuntime:       c.tuple.AgentRuntime,
				ModelLine:          c.tuple.ModelLine,
				EliminationReason:  c.eliminated,
			})
		}
	}
	return out
}

// poolLocators flattens one pipeline stage's pools across every leaf, in
// leaf order then candidate order, deduplicating locators that more than
// one leaf declares.
func poolLocators(p *plan, pool func(*leafPlan) []*candidate) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, leaf := range p.leaves {
		for _, c := range pool(leaf) {
			if seen[c.locator()] {
				continue
			}
			seen[c.locator()] = true
			out = append(out, c.locator())
		}
	}
	return out
}

// restorationSurvivors is the post-relent pool, which is the post-require
// pool when a relent happened and empty when none did. An empty list is the
// honest answer for a resolution no avoid ever narrowed: nothing was
// restored because nothing had been taken away.
func restorationSurvivors(p *plan) []string {
	restored := false
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			if c.restored {
				restored = true
			}
		}
	}
	if !restored {
		return []string{}
	}
	return poolLocators(p, func(l *leafPlan) []*candidate { return l.afterRequire })
}
