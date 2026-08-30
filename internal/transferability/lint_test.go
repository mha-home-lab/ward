package transferability

import "testing"

// TestScoreFieldCaseFixtures is the real acceptance test: the collatz/bowling
// batch (verbatim from the field case that forced this linter) MUST score as
// cheat-sheets; the positive-mod lesson (generalized mechanism + why) must NOT.
func TestScoreFieldCaseFixtures(t *testing.T) {
	cases := []struct {
		name       string
		topic      string
		content    string
		wantCheat  bool
		wantSignal bool // at least one fired signal (inspectability, not a mystery number)
	}{
		{
			name:      "collatz verbatim output",
			topic:     "collatz",
			content:   "collatz prints exactly 'Error: Only positive integers are allowed'",
			wantCheat: true,
		},
		{
			name:      "bowling slug instruction",
			topic:     "bowling",
			content:   "bowling take '-' for a miss",
			wantCheat: true,
		},
		{
			name:       "positive-mod generalized idiom",
			topic:      "positive-mod",
			content:    "positive-mod idiom (( x % m + m ) % m ); bash % truncates toward zero",
			wantCheat:  false,
			wantSignal: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Score(c.topic, "", c.content)
			if r.CheatSheet != c.wantCheat {
				t.Errorf("CheatSheet = %v, want %v (score=%d signals=%v)", r.CheatSheet, c.wantCheat, r.Score, r.Signals)
			}
			if c.wantSignal && len(r.Signals) == 0 {
				t.Errorf("expected fired signals for inspectability, got none")
			}
		})
	}
}

// TestScoreGeneralizationCap: one "because" can't paper over many verbatim
// strings — the generalization cap keeps a fragile chip honest.
func TestScoreGeneralizationCap(t *testing.T) {
	content := "because the mechanism is X; prints exactly 'a'; outputs exactly 'b'; returns exactly 'c'"
	r := Score("t", "", content)
	if r.Score > 0 {
		t.Errorf("cap failed: many verbatim strings must not be saved by one because (score=%d, signals=%v)", r.Score, r.Signals)
	}
	if !r.CheatSheet {
		t.Errorf("expected cheat-sheet, got %+v", r)
	}
}

// TestScoreCheatCap: the cheat term is capped at -5, so a heavily-cheat text
// bottoms out at -5 (or lower when generalization is absent) — never diverges.
func TestScorePureSignalBottomsOut(t *testing.T) {
	r := Score("x", "", "x prints exactly '1'; x prints exactly '2'; x prints exactly '3'; x prints exactly '4'; x prints exactly '5'; x prints exactly '6'")
	if r.Score != -5 {
		t.Errorf("cheat cap: expected -5, got %d", r.Score)
	}
}

// TestScoreLocalContentIsNotPortable: an ordinary local capture is deliberately
// NOT linted by the pipeline (scope decision), but the function itself flags
// verbatim per-instance phrasing so a caller choosing to score it sees it.
func TestScorePortableGeneralizationLanguage(t *testing.T) {
	r := Score("t", "", "any time you see the pattern, the lesson is to apply the idiom; because the mechanism is reusable")
	if r.CheatSheet {
		t.Errorf("generalization-rich text must not be a cheat-sheet: score=%d signals=%v", r.Score, r.Signals)
	}
	if r.Score < 3 { // capped at +3
		t.Errorf("expected +3 (capped) generalization contribution, got %d", r.Score)
	}
}
