package routing

import "testing"

func TestRoute(t *testing.T) {
	cases := []struct {
		name    string
		in      Inputs
		want    Tier
		cerem   string
		reject  bool
		escal   bool
	}{
		{"channel no-memory is cheap base but miss forces mid", Inputs{NodeKind: "channel"}, TierMid, "light", false, false},
		{"test verified+mem -> cheap", Inputs{NodeKind: "test", MemoryHit: true, Verify: "verified"}, TierCheap, "light", false, false},
		{"test no hit -> mid", Inputs{NodeKind: "test"}, TierMid, "light", false, false},
		{"test hit but stale -> not cheap", Inputs{NodeKind: "test", MemoryHit: true, Verify: "stale"}, TierMid, "light", false, false},
		{"test hit but error -> not cheap", Inputs{NodeKind: "test", MemoryHit: true, Verify: "error"}, TierMid, "light", false, false},
		{"test hit but unknown -> not cheap", Inputs{NodeKind: "test", MemoryHit: true, Verify: "unknown"}, TierMid, "light", false, false},
		{"approval no hit -> mid full", Inputs{NodeKind: "approval"}, TierMid, "full", false, false},
		{"channel contention -> strong full", Inputs{NodeKind: "channel", Contention: true}, TierStrong, "full", false, false},
		{"verified test + contention -> mid full", Inputs{NodeKind: "test", MemoryHit: true, Verify: "verified", Contention: true}, TierMid, "full", false, false},
		{"escalation 2 on miss -> strong", Inputs{NodeKind: "channel", Escalation: 2}, TierStrong, "full", false, true},
		{"escalation 3 -> reject", Inputs{NodeKind: "channel", Escalation: 3}, "", "", true, false},
		{"approval + contention -> strong full", Inputs{NodeKind: "approval", Contention: true}, TierStrong, "full", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Route(c.in)
			if d.Reject != c.reject {
				t.Fatalf("reject: got %v want %v (reason %q)", d.Reject, c.reject, d.Reason)
			}
			if c.reject {
				return
			}
			if d.Tier != c.want {
				t.Fatalf("tier: got %s want %s (reason %q)", d.Tier, c.want, d.Reason)
			}
			if d.Ceremony != c.cerem {
				t.Fatalf("ceremony: got %s want %s", d.Ceremony, c.cerem)
			}
			if d.Escalated != c.escal {
				t.Fatalf("escalated: got %v want %v (reason %q)", d.Escalated, c.escal, d.Reason)
			}
		})
	}
}
