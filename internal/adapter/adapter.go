// Package adapter turns a routing decision into actual work. The router is a
// pure function that selects a tier; this package is the execution half — it
// shells out to opencode (a local coding-agent CLI) at a free model chosen by
// tier, so WARD drives real agent work instead of only reporting what it would
// have done.
package adapter

import (
	"bytes"
	"os/exec"
)

// TierModel maps a routing tier to a free opencode model. The router's own
// model names (gemini-2.0-flash / gpt-4o-mini / gpt-4o) are illustrative;
// execution translates the tier to a concrete free model here.
var TierModel = map[string]string{
	"cheap":   "opencode/hy3-free",
	"mid":     "opencode/mimo-v2.5-free",
	"strong":  "opencode/nemotron-3-ultra-free",
}

// ModelForTier returns the free opencode model for a tier, defaulting to cheap.
func ModelForTier(tier string) string {
	if m, ok := TierModel[tier]; ok {
		return m
	}
	return TierModel["cheap"]
}

// Binary is the agent CLI invoked to perform work. Overridable for testing.
var Binary = "opencode"

// commandArgs builds the opencode run invocation. Exported for testing.
func commandArgs(repoRoot, model, prompt string) []string {
	return []string{"run", "-m", model, "--dir", repoRoot, prompt}
}

// Run drives a model at the given tier to perform prompt, returning its output.
// A non-zero exit is returned as an error so the engine can treat the work as
// failed (never silently successful).
func Run(repoRoot, model, prompt string) (string, error) {
	cmd := exec.Command(Binary, commandArgs(repoRoot, model, prompt)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}
