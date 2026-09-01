package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
