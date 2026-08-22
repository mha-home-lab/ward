package routing

import "fmt"

type Tier string

const (
	TierCheap  Tier = "cheap"
	TierMid    Tier = "mid"
	TierStrong Tier = "strong"
)

// Model registry for the v1 slice. Placeholder names; the engine records them
// as routing decisions only (no provider calls in v1).
var modelFor = map[Tier]string{
	TierCheap:  "gemini-2.0-flash",
	TierMid:    "gpt-4o-mini",
	TierStrong: "gpt-4o",
}

const MaxEscalation = 2

// Decision is the router output. Reject means escalation budget exhausted.
type Decision struct {
	Tier       Tier
	Model      string
	Ceremony   string // light | full
	MemoryHit  bool
	Verify     string
	Contention bool
	Escalated  bool
	Reject     bool
	Reason     string
}

// Inputs drive the pure routing function.
type Inputs struct {
	NodeKind   string // channel | approval | test | default
	MemoryHit  bool
	Verify     string // verified | stale | error | unknown
	Contention bool
	Escalation int // 0..MaxEscalation; >max => reject
}

func tierIndex(t Tier) int {
	switch t {
	case TierCheap:
		return 0
	case TierMid:
		return 1
	case TierStrong:
		return 2
	}
	return 0
}

func tierFromIndex(i int) Tier {
	switch i {
	case 2:
		return TierStrong
	case 1:
		return TierMid
	default:
		return TierCheap
	}
}

func maxTier(a, b Tier) Tier {
	if tierIndex(a) >= tierIndex(b) {
		return a
	}
	return b
}

// baseTier selects a tier before escalation / memory rules.
func baseTier(in Inputs) Tier {
	switch in.NodeKind {
	case "approval":
		return TierMid
	case "test":
		if in.MemoryHit && in.Verify == "verified" {
			return TierCheap
		}
		return TierMid
	case "channel":
		return TierCheap
	default:
		if in.MemoryHit && in.Verify == "verified" {
			return TierCheap
		}
		return TierMid
	}
}

// Route is a PURE function: same inputs -> same decision. No I/O, no LLM.
func Route(in Inputs) Decision {
	if in.Escalation > MaxEscalation {
		return Decision{
			Reject: true,
			Reason: fmt.Sprintf("escalation budget exhausted (max %d): cheap->mid->strong then human", MaxEscalation),
		}
	}

	t := baseTier(in)

	// Hard rule: a memory MISS (unverified / stale / unknown, or no hit) can
	// never vote for the cheap tier.
	if !in.MemoryHit || in.Verify != "verified" {
		t = maxTier(t, TierMid)
	}

	// Contention (real DAG overlap) escalates one tier and forces full ceremony.
	if in.Contention {
		t = tierFromIndex(min(tierIndex(t)+1, 2))
	}

	// Escalation after failure: each failed attempt bumps one tier (budgeted).
	escTier := tierFromIndex(min(in.Escalation, 2))
	escalated := tierIndex(escTier) > tierIndex(t)
	if escalated {
		t = escTier
	}

	ceremony := "light"
	if t == TierStrong || in.Contention || in.NodeKind == "approval" {
		ceremony = "full"
	}

	reason := "cheap+verified possible"
	if !in.MemoryHit {
		reason = "memory miss -> cannot use cheap"
	} else if in.Verify != "verified" {
		reason = "artifact not verified -> cannot use cheap"
	}
	if in.Contention {
		reason += "; contention -> +1 tier, full ceremony"
	}
	if escalated {
		reason += fmt.Sprintf("; escalated to %s after %d failure(s)", t, in.Escalation)
	}

	return Decision{
		Tier:       t,
		Model:      modelFor[t],
		Ceremony:   ceremony,
		MemoryHit:  in.MemoryHit,
		Verify:     in.Verify,
		Contention: in.Contention,
		Escalated:  escalated,
		Reason:     reason,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
