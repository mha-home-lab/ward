package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// skillCmd compiles verified store knowledge into pluggable agent skill files
// ("chips"): a SKILL.md an agent loader injects, making a cheap model sharp on
// ONE domain. Chips are DERIVED artifacts — compiled from the brain, never
// hand-edited; regenerate to refresh, check to detect staleness.
//
// Inclusion rule (thesis-consistent): status=accepted AND the artifact is
// trustworthy by its own class — work captures must be live-verified
// (verify_status=verified); verdict-knowledge (procedures/discoveries promoted
// through the R&D loop, which carry no verify_cmd) counts via architect
// promotion. Anything failing its class gate is excluded, not softened.
func skillCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "skill", Short: "compile verified knowledge into agent skill files (chips)"}
	cmd.AddCommand(skillPackCmd(), skillCheckCmd())
	return cmd
}

func skillPackCmd() *cobra.Command {
	var out, project, tag string
	var includeUnverified bool
	c := &cobra.Command{
		Use:   "pack <topic>",
		Short: "compile accepted knowledge for a topic into .opencode/skills/<chip>/SKILL.md",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("pack needs a topic"))
			}
			topic := strings.Join(args, " ")
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			var srcs []store.Artifact
			if tag != "" {
				// Exact-tag mode: deterministic compilation surface. FTS fuzz
				// is fine inside one repo but pollutes GLOBAL chips with
				// off-topic artifacts (bit live during the first global emit).
				srcs, err = s.SearchArtifactsTagged("", "", project, tag, 50)
			} else {
				srcs, err = skillSources(s, topic, project, includeUnverified)
			}
			if err != nil {
				return failErr(err)
			}
			if len(srcs) == 0 {
				return failErr(fmt.Errorf("no eligible knowledge for %q (need accepted+trusted artifacts)", topic))
			}

			chipName := chipNameFor(topic)
			dir := out
			if dir == "" {
				dir = filepath.Join(".opencode", "skills", chipName)
			}
			path := filepath.Join(dir, "SKILL.md")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return failErr(err)
			}
			body := renderChip(chipName, topic, s.Home, srcs)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]any{"path": path, "sources": len(srcs), "topic": topic})
			} else {
				printLine(fmt.Sprintf("compiled %d knowledge artifact(s) -> %s", len(srcs), path))
				for _, a := range srcs {
					printLine("  source: " + a.ID + " (" + a.Kind + ", verify=" + gateLabel(a) + ")")
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&out, "out", "", "output directory (default .opencode/skills/<chip>; use ~/.config/opencode/skills/<name> for a GLOBAL chip)")
	c.Flags().StringVar(&tag, "tag", "", "exact-tag compilation (portable knowledge only; recommended for global chips)")
	c.Flags().StringVar(&project, "project", "", "project lens filter")
	c.Flags().BoolVar(&includeUnverified, "include-unverified", false, "also compile trusted-class artifacts whose live verify failed or never ran (marked UNVERIFIED in the chip)")
	return c
}

// skillCheckCmd reports whether a previously emitted chip is stale: any source
// artifact whose verify_status moved after compilation (or that got superseded)
// means the chip is teaching stale facts — recompile.
func skillCheckCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "check <chip-dir>",
		Short: "report whether a compiled chip's sources have drifted (stale chip = recompile)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			path := filepath.Join(args[0], "SKILL.md")
			data, err := os.ReadFile(path)
			if err != nil {
				return failErr(err)
			}
			// Follow the chip's store locator if present: a GLOBAL chip is
			// compiled from one oracle store; checking it against whatever
			// cwd's store is store-blind (field report bug 3).
			prevHome := os.Getenv("WARD_HOME")
			locator := chipStoreLocator(string(data))
			if locator != "" {
				os.Setenv("WARD_HOME", locator)
			}
			s, err := store.Open()
			if err != nil {
				if locator != "" {
					os.Setenv("WARD_HOME", prevHome)
				}
				return failErr(err)
			}

			stale, missing := 0, 0
			var lines []string
			for _, id := range chipSourceIDs(string(data)) {
				a, err := s.GetArtifact(id)
				if err != nil {
					missing++
					lines = append(lines, fmt.Sprintf("  MISSING %s", id))
					continue
				}
				if a.Status == "superseded" || a.VerifyStatus == "stale" || a.VerifyStatus == "error" {
					stale++
					lines = append(lines, fmt.Sprintf("  DRIFTED %s (%s, verify=%s)", a.ID, a.Status, a.VerifyStatus))
				}
			}
			s.DB.Close()
			if locator != "" {
				os.Setenv("WARD_HOME", prevHome)
			}
			res := map[string]any{"chip": path, "sources_drifted": stale, "sources_missing": missing,
				"verdict": map[bool]string{true: "FRESH", false: "STALE — rerun: ward skill pack"}[stale == 0 && missing == 0]}
			if jsonOut {
				printJSON(res)
			} else {
				printLine(fmt.Sprintf("%s: %s", path, res["verdict"]))
				for _, l := range lines {
					printLine(l)
				}
			}
			return nil
		},
	}
	return c
}

// skillSources selects eligible artifacts for a chip: accepted, matching the
// topic by tag or search, passing their trust class.
func skillSources(s *store.Store, topic, project string, includeUnverified bool) ([]store.Artifact, error) {
	res, err := s.SearchArtifacts(topic, "", project, 30)
	if err != nil {
		return nil, err
	}
	var out []store.Artifact
	for _, a := range res {
		if a.Status != "accepted" {
			continue
		}
		trusted := a.VerifyCmd == "" || a.VerifyStatus == "verified"
		if !trusted && !includeUnverified {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, nil
}

func gateLabel(a store.Artifact) string {
	if a.VerifyCmd == "" {
		return "promoted"
	}
	return a.VerifyStatus
}

func chipNameFor(topic string) string {
	repl := func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}
	n := strings.Map(repl, strings.ToLower(topic))
	n = strings.Trim(n, "-")
	for strings.Contains(n, "--") {
		n = strings.ReplaceAll(n, "--", "-")
	}
	if n == "" {
		n = "chip"
	}
	if !strings.HasPrefix(n, "ward-") {
		n = "ward-" + n
	}
	return n
}

// sanitizeFrontmatter strips control characters (notably newlines) so a topic
// can never break out of the YAML frontmatter block.
func sanitizeFrontmatter(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, s)
}

// renderChip produces SKILL.md: frontmatter (loader-compatible) + compact
// knowledge body grouped by artifact kind + provenance footer. Every claim
// carries its source id so audit stays one hop away, and the footer names the
// STORE the sources live in - global chips are compiled from one oracle store,
// and 'ward skill check' must follow that locator instead of whatever cwd
// happens to be (field report bug 3).
func renderChip(name, topic, homeStore string, srcs []store.Artifact) string {
	topic = sanitizeFrontmatter(topic)
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: Ward-compiled chip for %q — verified knowledge from this project's brain. Use when working on %s in this repo.\n---\n\n", name, topic, topic)
	b.WriteString("# " + name + "\n\n")
	b.WriteString("> Compiled by `ward skill pack` from the project brain. DO NOT hand-edit: regenerate instead.\n")
	b.WriteString("> Claims below were gated by the store (live-verified work results and/or architect-promoted knowledge).\n\n")

	kinds := []struct {
		key   string
		title string
	}{
		{"solution", "Procedures (how to do it here)"},
		{"discovery", "Field notes (how the world does it)"},
		{"feedback", "Watch out (critiques and dead ends)"},
		{"context", "Background"},
	}
	for _, k := range kinds {
		var group []store.Artifact
		for _, a := range srcs {
			if a.Kind == k.key {
				group = append(group, a)
			}
		}
		if len(group) == 0 {
			continue
		}
		b.WriteString("## " + k.title + "\n\n")
		for _, a := range group {
			fmt.Fprintf(&b, "### %s\n\n", a.Summary)
			gate := ""
			if a.VerifyCmd != "" && a.VerifyStatus != "verified" {
				gate = " **[UNVERIFIED — treat as suspect]**"
			}
			fmt.Fprintf(&b, "%s%s\n\n", strings.TrimSpace(a.Content), gate)
		}
	}

	b.WriteString("---\n\n## Sources (audit trail)\n\n")
	if homeStore != "" {
		b.WriteString("store: " + homeStore + "\n\n")
	}
	b.WriteString("| id | kind | gate | verify_at |\n|---|---|---|---|\n")
	for _, a := range srcs {
		va := a.VerifyAt
		if va == "" {
			va = "-"
		} else if t, err := time.Parse("2006-01-02T15:04:05Z", va); err == nil {
			va = t.Format("2006-01-02")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", a.ID, a.Kind, gateLabel(a), va)
	}
	fmt.Fprintf(&b, "\nRecompile: ward skill pack %q ; staleness: ward skill check %s\n", topic, name)
	return b.String()
}

// chipSourceIDs extracts source artifact ids from a rendered chip's table.
func chipSourceIDs(body string) []string {
	var ids []string
	inTable := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## Sources") {
			inTable = true
			continue
		}
		if !inTable || !strings.HasPrefix(line, "| ") {
			if inTable && line != "" && !strings.HasPrefix(line, "|") {
				break
			}
			continue
		}
		cols := strings.Split(strings.Trim(line, "| "), "|")
		if len(cols) >= 3 {
			id := strings.TrimSpace(cols[0])
			if id == "id" || strings.HasPrefix(id, "-") {
				continue
			}
			ids = append(ids, id)
		}
	}
	return ids
}

// chipStoreLocator extracts the oracle-store path from a rendered chip.
func chipStoreLocator(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "store: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "store: "))
		}
		if strings.HasPrefix(line, "| id |") {
			break
		}
	}
	return ""
}
