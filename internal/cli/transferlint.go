package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/transferability"
)

// portableTopicName extracts the topic part of a portable tag, honoring both
// `portable:<name>` and `topic:portable:<name>`. Returns "" if the tag does not
// carry the portable marker.
func portableTopicName(tag string) string {
	if i := strings.Index(tag, "portable:"); i >= 0 {
		return tag[i+len("portable:"):]
	}
	return ""
}

// warnIfCheatSheet is the shared transferability lint for the portable:
// pipeline. It fires only for the FIRST portable:* tag, and warns (never
// blocks, never changes the artifact's status) when the content reads like a
// repo-specific cheat-sheet instead of a generalizable mechanism. The hard
// gate lives at pack/skill-sync; this is the heuristic at the point of least
// context, identical for the automatic capture path (captureNode) and the
// manual path (memoryPutCmd) so a silent bypass can't hide the signal.
func warnIfCheatSheet(tags []string, summary, content string) {
	for _, t := range tags {
		if name := portableTopicName(t); name != "" {
			if r := transferability.Score(name, summary, content); r.CheatSheet {
				fmt.Fprintln(os.Stderr, "warning: "+t+" capture looks instance-specific (cheat-sheet score "+strconv.Itoa(r.Score)+"); rewrite as a generalizable mechanism before packing to the global vault")
				for _, s := range r.Signals {
					fmt.Fprintln(os.Stderr, "  - "+s)
				}
			}
			return
		}
	}
}

// previewTransferability is the write-once feedback gate: it scores a
// portable:* capture the SAME way the pack gate will decide it and prints the
// score + fired signals + a one-line verdict BEFORE the artifact is written.
// It resolves the first portable tag exactly like warnIfCheatSheet so preview
// and the real put never disagree about which tag is scored. Returns whether it
// printed (i.e. a portable tag was present). Used by `memory put --dry-run`.
func previewTransferability(tags []string, summary, content string) (portalable string, printed bool) {
	for _, t := range tags {
		if name := portableTopicName(t); name != "" {
			r := transferability.Score(name, summary, content)
			verdict := "would PASS the transferability gate"
			if r.CheatSheet {
				verdict = "would FAIL the transferability gate (instance-specific)"
			}
			fmt.Fprintf(os.Stderr, "transferability %s: score %d (%s)\n", t, r.Score, verdict)
			for _, s := range r.Signals {
				fmt.Fprintf(os.Stderr, "  - %s\n", s)
			}
			if r.CheatSheet {
				fmt.Fprintln(os.Stderr, "  hint: add a causal 'because ...' hinge or let the body's concrete mechanism stand on its own density; avoid verbatim output / paths in a portable chip")
			}
			return name, true
		}
	}
	return "", false
}

// hintIfRecurrence is the assistive recurrence autocomplete (signal 5): when a
// new portable:* capture shares enough distinctive tokens with an existing
// artifact under the SAME topic, it returns a non-blocking hint suggesting
// `--recurs <id>` for the true-recurrence case. It never links anything —
// only the agent's explicit --recurs does. It deliberately under-fires (real
// recurrences with little lexical overlap slip past) and may over-fire on
// coincidence; it is autocomplete, not detection, so it changes no data and
// blocks nothing. Returns "" when there is no portable tag, no similarly-worded
// sibling, or the caller already supplied --recurs (caller gates on that).
func hintIfRecurrence(s *store.Store, tags []string, content string, newID string) string {
	for _, t := range tags {
		name := portableTopicName(t)
		if name == "" {
			continue
		}
		srcs, err := s.ArtifactsForPortableTopic(name)
		if err != nil {
			return ""
		}
		bestID, bestN := "", 0
		for _, a := range srcs {
			if a.ID == newID || a.Kind == "claim" {
				continue
			}
			if n := transferability.SharedDistinctiveTokens(content, a.Content); n > bestN {
				bestID, bestN = a.ID, n
			}
		}
		if bestN >= 3 && bestID != "" {
			return fmt.Sprintf("this looks similar to %s (%d shared tokens) — if it's the same lesson in different wording, consider --recurs %s instead of a fresh capture", bestID, bestN, bestID)
		}
		return ""
	}
	return ""
}
