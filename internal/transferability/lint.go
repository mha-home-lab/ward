// Package transferability lints portable-knowledge artifacts for
// cross-project usefulness. A portable chip must hold a generalized mechanism
// + the hard lesson + why it matters — NOT a repo-specific cheat-sheet (verbatim
// output strings, exact file paths, per-exercise argv). This is a deterministic,
// pattern-based linter (no model call), deliberately cheap and occasionally
// wrong — the same posture as `go vet`, not a black-box quality oracle.
package transferability

import (
	"regexp"
	"strconv"
	"strings"
)

// LintResult is the output of Score. Score <= 0 means the text reads like a
// cheat-sheet (instance-specific) and is not fit for the global vault.
type LintResult struct {
	Score      int      // generalization signals − cheat-sheet signals
	CheatSheet bool     // Score <= 0
	Signals    []string // human-readable, one per pattern that fired (the --why output)
}

// generalizationWords are the portable signals: language that frames a
// mechanism as reusable across projects rather than a one-off answer. Capped at
// +3 so a single well-placed "because" can't paper over many verbatim strings.
var generalizationWords = []string{
	"idiom",
	"the pattern",
	"in general",
	"any time",
	"whenever",
	"because",
	"the trap is",
	"the mechanism",
	"the lesson",
}

// verbatimRE catches "prints/outputs/returns exactly '<quoted string>'" — the
// collatz field failure verbatim. The quoted string after "exactly" is what
// makes it an answer-copy instead of a generalization.
var verbatimRE = regexp.MustCompile(`(?i)\b(?:prints?|outputs?|returns?)\s+exactly\s*["']`)

// pathRE catches path-shaped tokens like "exercises/collatz/main.go".
var pathRE = regexp.MustCompile(`\b\w[\w\-]*/[\w\-\.]+\.\w+\b`)

// argvRE catches argv-shaped references ("argv[0]", "argv[1]").
var argvRE = regexp.MustCompile(`\bargv\[`)

// stopwords are common words that would otherwise be mistaken for repeated
// exercise-slug identifiers (a "the ... the ..." sentence is not a cheat-sheet).
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "this": true, "that": true,
	"with": true, "from": true, "you": true, "your": true, "are": true,
	"file": true, "files": true, "a": true, "an": true, "to": true, "of": true,
	"in": true, "is": true, "it": true, "on": true, "at": true,
}

// slugRE is a single lowercase identifier token (letters/digits, optional
// internal hyphens) — the shape of an exercise slug like "collatz" or "bowling".
var slugRE = regexp.MustCompile(`\b[a-z][a-z0-9]{1,}(?:-[a-z0-9]+)*\b`)

// Score evaluates topic, summary and content together for transferability.
// It is a PURE function: same inputs, same result. No I/O, no model call.
func Score(topic, summary, content string) LintResult {
	text := strings.ToLower(topic + "\n" + summary + "\n" + content)
	var signals []string
	cheat := 0
	gen := 0

	// Cheat-sheet: verbatim-output phrasing.
	for _, m := range verbatimRE.FindAllString(text, -1) {
		cheat++
		signals = append(signals, "verbatim-output phrasing: "+strings.TrimSpace(m))
	}
	// Cheat-sheet: instance-specific path tokens.
	for _, m := range pathRE.FindAllString(text, -1) {
		cheat++
		signals = append(signals, "instance-specific path: "+m)
	}
	// Cheat-sheet: argv indexing.
	for _, m := range argvRE.FindAllString(text, -1) {
		cheat++
		signals = append(signals, "argv indexing: "+m)
	}
	// Cheat-sheet: a bare exercise-slug identifier repeated 2+ times with no
	// nearby generalization word (the collatz/bowling batch shape).
	if !hasGeneralization(text) {
		if slug, n := repeatedSlug(text); n >= 2 {
			cheat++
			signals = append(signals, "exercise-slug "+slug+" repeated "+itoa(n)+"x with no generalization language")
		}
	}

	// Portable: generalization language. Capped contribution at +3.
	for _, w := range generalizationWords {
		if strings.Contains(text, w) {
			gen++
			signals = append(signals, "generalization: "+w)
		}
	}

	score := min(gen, 3) - min(cheat, 5)
	return LintResult{
		Score:      score,
		CheatSheet: score <= 0,
		Signals:    signals,
	}
}

// hasGeneralization reports whether any generalization word appears in text.
func hasGeneralization(text string) bool {
	for _, w := range generalizationWords {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// repeatedSlug returns the most-repeated identifier-like token and its count,
// or ("", 0) if none repeats. Only meaningful when the caller has already
// confirmed there is no generalization language nearby.
func repeatedSlug(text string) (string, int) {
	counts := map[string]int{}
	order := []string{}
	for _, tok := range slugRE.FindAllString(text, -1) {
		if len(tok) < 2 || stopwords[tok] {
			continue
		}
		if _, ok := counts[tok]; !ok {
			order = append(order, tok)
		}
		counts[tok]++
	}
	best, n := "", 0
	for _, tok := range order {
		if counts[tok] > n {
			best, n = tok, counts[tok]
		}
	}
	if n < 2 {
		return "", 0
	}
	return best, n
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// Tokens returns the distinctive identifier tokens in text: lowercased,
// single-alphanumeric identifiers with internal hyphens, minus the cheat-sheet
// linter's stopword list. It is the same tokenizer Score uses, exposed so the
// capture-time recurrence hint (a non-blocking autocomplete, never a link)
// reuses the exact vocabulary instead of a second, divergent tokenizer.
func Tokens(text string) []string {
	var out []string
	for _, tok := range slugRE.FindAllString(strings.ToLower(text), -1) {
		if len(tok) < 2 || stopwords[tok] {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// SharedDistinctiveTokens returns how many distinctive tokens appear in BOTH
// texts (the size of the intersection of their token sets). Used by the
// assistive recurrence hint as a lazy, transparent similarity signal. It is
// explicitly not a judge: it under-fires on real recurrences with little lexical
// overlap and may over-fire on coincidental wording — hence the hint never
// links anything itself.
func SharedDistinctiveTokens(a, b string) int {
	sa := map[string]bool{}
	for _, t := range Tokens(a) {
		sa[t] = true
	}
	n := 0
	for _, t := range Tokens(b) {
		if sa[t] {
			n++
		}
	}
	return n
}
