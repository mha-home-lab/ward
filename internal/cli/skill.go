package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/transferability"
	"github.com/mha-home-lab/ward/internal/verification"
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
	cmd := &cobra.Command{Use: "skill", Short: "compile verified knowledge into agent skill files (chips); localize global chips into this repo"}
	cmd.AddCommand(skillPackCmd(), skillCheckCmd(), skillInstallCmd(), skillListGlobalCmd(), skillLintCmd())
	return cmd
}

func skillPackCmd() *cobra.Command {
	var out, project, tag, reason string
	var includeUnverified, force bool
	c := &cobra.Command{
		Use:   "pack <topic>",
		Short: "compile accepted knowledge for a topic into .opencode/skills/<chip>/SKILL.md",
		Example: `  ward skill pack rd:checks
  ward skill pack agent-reliability --tag portable:agent-reliability --out ~/.config/opencode/skills/ward-agent-reliability
  ward skill pack auth --project secure-bank --json
  ward skill pack portable:bash --tag portable:bash --out ~/.config/opencode/skills/ward-bash --force --reason "ship anyway"`,
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

			// Transferability gate: only the portable pipeline is linted.
			// Ordinary local captures are legitimately instance-specific and
			// are never scored.
			portable := strings.HasPrefix(tag, "portable:") || isGlobalSkillsOut(out)
			var g gateOutcome
			if portable {
				var err error
				if g, err = gateTransferability(s, &srcs, force, reason); err != nil {
					return failErr(err)
				}
				if len(srcs) == 0 {
					if len(g.removed) > 0 {
						var ids []string
						for _, a := range g.removed {
							ids = append(ids, a.ID)
						}
						return failErr(fmt.Errorf("pack vetoed: portable bundle for %q would be empty — %d source(s) scored as instance-specific cheat-sheets: %s", topic, len(g.removed), strings.Join(ids, ", ")))
					}
					return failErr(fmt.Errorf("no eligible knowledge for %q (need accepted+trusted artifacts)", topic))
				}
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
				res := map[string]any{"path": path, "sources": len(srcs), "topic": topic}
				if len(g.removed) > 0 {
					inst := []map[string]any{}
					for i, a := range g.removed {
						inst = append(inst, map[string]any{"id": a.ID, "summary": a.Summary, "score": g.removedScores[i].Score, "signals": g.removedScores[i].Signals})
					}
					res["instance_specific"] = inst
					res["not_synced_to_global_vault"] = len(g.removed)
				}
				if len(g.overridden) > 0 {
					ovr := []map[string]any{}
					for i, a := range g.overridden {
						ovr = append(ovr, map[string]any{"id": a.ID, "summary": a.Summary, "score": g.overriddenScores[i].Score, "reason": reason, "signals": g.overriddenScores[i].Signals})
					}
					res["force_included_with_reason"] = ovr
				}
				printJSON(res)
			} else {
				printLine(fmt.Sprintf("compiled %d knowledge artifact(s) -> %s", len(srcs), path))
				for _, a := range srcs {
					printLine("  source: " + a.ID + " (" + a.Kind + ", verify=" + gateLabel(a) + ")")
				}
				for i, a := range g.removed {
					printLine(fmt.Sprintf("  EXCLUDED %s: instance-specific, not synced to the global vault (score %d)", a.ID, g.removedScores[i].Score))
					for _, sgn := range g.removedScores[i].Signals {
						printLine("      - " + sgn)
					}
				}
				for i, a := range g.overridden {
					printLine(fmt.Sprintf("  FORCED %s: instance-specific cheat-sheet synced anyway (reason: %s)", a.ID, reason))
					for _, sgn := range g.overriddenScores[i].Signals {
						printLine("      - " + sgn)
					}
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&out, "out", "", "output directory (default .opencode/skills/<chip>; use ~/.config/opencode/skills/<name> for a GLOBAL chip)")
	c.Flags().StringVar(&tag, "tag", "", "exact-tag compilation (portable knowledge only; recommended for global chips)")
	c.Flags().StringVar(&project, "project", "", "project lens filter")
	c.Flags().StringVar(&reason, "reason", "", "required with --force: why a cheat-sheet source is being synced to the global vault anyway")
	c.Flags().BoolVar(&includeUnverified, "include-unverified", false, "also compile trusted-class artifacts whose live verify failed or never ran (marked UNVERIFIED in the chip)")
	c.Flags().BoolVar(&force, "force", false, "bypass the transferability lint for a portable pack (needs --reason)")
	return c
}

// gateOutcome reports how the transferability gate split a candidate source
// list. The two cheat-sheet groups are mutually exclusive:
//   - removed:    cheat-sheet sources EXCLUDED from the bundle (not synced).
//   - overridden: cheat-sheet sources force-included (--force --reason) with a
//     recorded reason; they ARE part of the bundle and must be reported as
//     synced, not "not synced", or the output would lie.
type gateOutcome struct {
	removed          []store.Artifact
	removedScores    []transferability.LintResult
	overridden       []store.Artifact
	overriddenScores []transferability.LintResult
}

// gateTransferability splits the candidate sources for a portable pack. When
// not forced, cheat-sheet-scored (instance-specific) sources are removed from
// srcs and reported in outcome.removed, so they are visibly reported but NOT
// synced to the global vault. When forced (--force --reason), the reason is not
// optional, every source is kept in srcs, and the cheat-sheet ones are reported
// in outcome.overridden with the reason recorded on each artifact so the
// exception stays auditable.
func gateTransferability(s *store.Store, srcs *[]store.Artifact, force bool, reason string) (gateOutcome, error) {
	var out gateOutcome
	if force {
		if reason == "" {
			return out, fmt.Errorf("--force needs --reason: an override without a logged reason is exactly the silent exception the lint gate exists to prevent")
		}
		for _, a := range *srcs {
			r := transferability.Score(a.Summary, a.Summary, a.Content)
			if r.CheatSheet {
				if err := s.SetOverrideReason(a.ID, reason); err != nil {
					return out, err
				}
				out.overridden = append(out.overridden, a)
				out.overriddenScores = append(out.overriddenScores, r)
			}
		}
		return out, nil
	}
	kept := (*srcs)[:0]
	for _, a := range *srcs {
		r := transferability.Score(a.Summary, a.Summary, a.Content)
		if r.CheatSheet {
			out.removed = append(out.removed, a)
			out.removedScores = append(out.removedScores, r)
			continue
		}
		kept = append(kept, a)
	}
	*srcs = kept
	return out, nil
}

// isGlobalSkillsOut reports whether an --out directory targets the global
// skills vault (~/.config/opencode/skills/...), the point where knowledge
// actually leaves the repo boundary and the transportability gate must fire.
func isGlobalSkillsOut(out string) bool {
	if out == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	global := filepath.Join(home, ".config", "opencode", "skills")
	absOut := out
	if !filepath.IsAbs(absOut) {
		abs, aerr := filepath.Abs(out)
		if aerr != nil {
			return false
		}
		absOut = abs
	}
	rel, err := filepath.Rel(global, absOut)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// skillCheckCmd reports whether a previously emitted chip is stale: any source
// artifact whose verify_status moved after compilation (or that got superseded)
// means the chip is teaching stale facts — recompile.
func skillCheckCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "check <chip-dir>",
		Short: "report whether a compiled chip's sources have drifted (stale chip = recompile)",
		Example: `  ward skill check .opencode/skills/ward-rd-checks
  ward skill check ward-agent-reliability
  ward skill check .opencode/skills/ward-auth --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			arg := args[0]
			var path string
			var data []byte
			err := fmt.Errorf("no such chip")
			candidates := []string{filepath.Join(arg, "SKILL.md")}
			if !strings.Contains(arg, string(filepath.Separator)) {
				// Bare chip name: also look in the global skills dir (field
				// report residual - global chips are cwd-independent by
				// design; lookup must be too).
				if home, herr := os.UserHomeDir(); herr == nil {
					candidates = append(candidates,
						filepath.Join(home, ".config", "opencode", "skills", arg, "SKILL.md"))
				}
			}
			for _, cand := range candidates {
				var d []byte
				if d, err = os.ReadFile(cand); err == nil {
					data, path = d, cand
					break
				}
			}
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

// skillLintCmd re-scores a compiled chip's LIVE source artifacts for
// transferability. It resolves the chip back to its sources via chipSourceIDs,
// then scores each against CURRENT artifact content (not the frozen chip text,
// so it also catches a chip that drifted into cheat-sheet territory after
// compilation). Prints a scorecard: portable / borderline / cheat-sheet counts,
// with --why for per-source signals. Exit is non-zero if any source currently
// scores as a cheat-sheet.
func skillLintCmd() *cobra.Command {
	var why bool
	c := &cobra.Command{
		Use:   "lint <chip>",
		Short: "re-score a chip's live sources for transferability (catches drift into cheat-sheet territory)",
		Example: `  ward skill lint ward-bash
  ward skill lint ward-bash --why
  ward skill lint .opencode/skills/ward-rd-checks --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			arg := args[0]
			var path string
			var data []byte
			err := fmt.Errorf("no such chip")
			candidates := []string{filepath.Join(arg, "SKILL.md")}
			if !strings.Contains(arg, string(filepath.Separator)) {
				if home, herr := os.UserHomeDir(); herr == nil {
					candidates = append(candidates,
						filepath.Join(home, ".config", "opencode", "skills", arg, "SKILL.md"))
				}
			}
			for _, cand := range candidates {
				var d []byte
				if d, err = os.ReadFile(cand); err == nil {
					data, path = d, cand
					break
				}
			}
			if err != nil {
				return failErr(err)
			}
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

			type entry struct {
				id      string
				summary string
				r       transferability.LintResult
				missing bool
			}
			var entries []entry
			for _, id := range chipSourceIDs(string(data)) {
				a, gerr := s.GetArtifact(id)
				if gerr != nil {
					entries = append(entries, entry{id: id, missing: true})
					continue
				}
				entries = append(entries, entry{id: id, summary: a.Summary, r: transferability.Score(a.Summary, a.Summary, a.Content)})
			}
			s.DB.Close()
			if locator != "" {
				os.Setenv("WARD_HOME", prevHome)
			}

			portable, borderline, cheat := 0, 0, 0
			for _, e := range entries {
				switch {
				case e.missing:
					// not part of the transferability verdict
				case e.r.CheatSheet:
					cheat++
				case e.r.Score == 1:
					borderline++
				default:
					portable++
				}
			}
			res := map[string]any{
				"chip":        path,
				"portable":    portable,
				"borderline":  borderline,
				"cheat_sheet": cheat,
			}
			bad := 0
			if jsonOut {
				var detail []map[string]any
				for _, e := range entries {
					row := map[string]any{"id": e.id}
					if e.missing {
						row["status"] = "missing"
					} else {
						row["status"] = map[bool]string{true: "cheat_sheet", false: "ok"}[e.r.CheatSheet]
						row["score"] = e.r.Score
						if e.summary != "" {
							row["summary"] = e.summary
						}
						if why {
							row["signals"] = e.r.Signals
						}
						if e.r.CheatSheet {
							bad++
						}
					}
					detail = append(detail, row)
				}
				res["sources"] = detail
				printJSON(res)
			} else {
				printLine(fmt.Sprintf("%s: portable=%d borderline=%d cheat-sheet=%d", path, portable, borderline, cheat))
				for _, e := range entries {
					switch {
					case e.missing:
						printLine(fmt.Sprintf("  MISSING %s", e.id))
					case e.r.CheatSheet:
						printLine(fmt.Sprintf("  CHEAT-SHEET %s (score %d)", e.id, e.r.Score))
						if why {
							for _, sgn := range e.r.Signals {
								printLine("      - " + sgn)
							}
						}
						bad++
					case e.r.Score == 1:
						printLine(fmt.Sprintf("  borderline %s (score %d)", e.id, e.r.Score))
						if why {
							for _, sgn := range e.r.Signals {
								printLine("      - " + sgn)
							}
						}
					default:
						printLine(fmt.Sprintf("  portable %s (score %d)", e.id, e.r.Score))
						if why {
							for _, sgn := range e.r.Signals {
								printLine("      - " + sgn)
							}
						}
					}
				}
			}
			if bad > 0 {
				return failErr(fmt.Errorf("%d chip source(s) scored as instance-specific cheat-sheets; rewrite them as generalizable mechanisms before syncing to the global vault", bad))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&why, "why", false, "print the fired transferability signals per source")
	return c
}

// skillInstallCmd closes the feedforward loop (control-skill-localize): a
// GLOBAL portable chip becomes a FRESH local claim in THIS repo's store —
// Local=true, user-supplied verify_cmd, live-verified immediately. One
// artifact per chip; the chip's sources stay untouched in their home store.
// When the gate passes the artifact votes cheap on its topic in future runs;
// when it fails the artifact exists but does NOT vote cheap, and install errors.
func skillInstallCmd() *cobra.Command {
	var verifyCmd, dir, by string
	c := &cobra.Command{
		Use:   "install <topic> --verify-cmd <cmd>",
		Short: "localize a global skill chip into this repo as a verified local artifact",
		Example: `  ward skill install portable:control-antiwindup --verify-cmd "go test ./internal/store/... -run TestSweepExpiredClaims"
  ward skill list-global`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("skill install needs a topic"))
			}
			topic := args[0]
			if verifyCmd == "" {
				return failErr(fmt.Errorf("install needs --verify-cmd: a gate that proves THIS repo's claim; a missing gate is a phantom success"))
			}
			if isTrivialVerify(verifyCmd) {
				return failErr(fmt.Errorf("rejected: --verify-cmd %q is a phantom gate (true/false/: prove nothing); provide a gate that exercises the claim", verifyCmd))
			}
			path, err := findGlobalChip(topic, dir)
			if err != nil {
				return failErr(err)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return failErr(err)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			a := store.Artifact{
				Kind:       "solution",
				Summary:    fmt.Sprintf("localized skill: %s (chip %s)", topic, filepath.Base(filepath.Dir(path))),
				Content:    string(body),
				Tags:       []string{"topic:" + topic},
				CreatedBy:  by,
				Local:      true,
				VerifyCmd:  verifyCmd,
				VerifyKind: "shell",
				Ceremony:   "light",
			}
			id, err := s.UpsertArtifact(a)
			if err != nil {
				return failErr(err)
			}
			// UpsertArtifact's id is (kind,summary,content)-derived, so
			// re-installing the same chip reuses the row: persist the CURRENT
			// gate so the stored verify_cmd always equals the gate that
			// produced the stored status (gate and verdict never diverge).
			if err := s.SetVerifyCmd(id, verifyCmd, "shell"); err != nil {
				return failErr(err)
			}
			if _, err := s.Promote([]string{id}, "auto-accept (light ceremony)", by); err != nil {
				return failErr(err)
			}
			repo, _ := os.Getwd()
			res := verification.Run(a, repo)
			if res.Status == "verified" {
				if err := s.SetVerify(id, "verified"); err != nil {
					return failErr(err)
				}
			} else {
				if err := s.SetVerify(id, "error"); err != nil {
					return failErr(err)
				}
			}
			if jsonOut {
				printJSON(map[string]any{"id": id, "topic": topic, "local": true, "verify_status": res.Status, "chip": path})
				if res.Status != "verified" {
					return failErr(fmt.Errorf("verify failed: %s", res.Detail))
				}
				return nil
			}
			if res.Status == "verified" {
				printLine(fmt.Sprintf("installed %s (localized %s; verify=%s) — can now vote cheap on its topic", id, topic, res.Status))
			} else {
				printLine(fmt.Sprintf("installed %s (%s; verify=%s FAILED: %s)", id, topic, res.Status, res.Detail))
				printLine("  the artifact exists but does NOT vote cheap until its gate passes")
				return failErr(fmt.Errorf("verify failed: %s", res.Detail))
			}
			return nil
		},
	}
	c.Flags().StringVar(&verifyCmd, "verify-cmd", "", "verification command proving THIS repo's claim (required)")
	c.Flags().StringVar(&dir, "dir", "", "override global skills directory (default ~/.config/opencode/skills)")
	c.Flags().StringVar(&by, "by", "agent", "creator name")
	return c
}

// skillListGlobalCmd lists portable skill chips available in the global skills
// directory — the install surface for `ward skill install`.
func skillListGlobalCmd() *cobra.Command {
	var dir string
	c := &cobra.Command{
		Use:     "list-global",
		Short:   "list portable skill chips in the global skills directory",
		Example: "  ward skill list-global\n  ward skill list-global --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			base := dir
			if base == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return failErr(err)
				}
				base = filepath.Join(home, ".config", "opencode", "skills")
			}
			chips := []map[string]string{}
			if entries, err := os.ReadDir(base); err == nil {
				var names []string
				for _, e := range entries {
					if e.IsDir() && strings.HasPrefix(e.Name(), "ward-") {
						names = append(names, e.Name())
					}
				}
				sort.Strings(names)
				for _, n := range names {
					chips = append(chips, map[string]string{"name": n, "path": filepath.Join(base, n, "SKILL.md")})
				}
			}
			if jsonOut {
				printJSON(map[string]any{"dir": base, "chips": chips})
				return nil
			}
			printLine("global skills at " + base)
			if len(chips) == 0 {
				printLine("  (no ward-* chips — seed with 'ward skill-sync')")
			}
			for _, c := range chips {
				printLine(fmt.Sprintf("  %-24s %s", c["name"], c["path"]))
			}
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "override global skills directory")
	return c
}

// findGlobalChip resolves the SKILL.md for a topic under the global skills
// directory. Global chips are named chipNameFor(topic-without-portable:)
// (matching skill-sync), with a fallback to the raw topic form.
func findGlobalChip(topic, override string) (string, error) {
	base := override
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config", "opencode", "skills")
	}
	for _, name := range []string{chipNameFor(strings.TrimPrefix(topic, "portable:")), chipNameFor(topic)} {
		p := filepath.Join(base, name, "SKILL.md")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("no global chip for %q under %s (seed with 'ward skill-sync', or see 'ward skill list-global')", topic, base)
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

// chipSourceIDs extracts source artifact ids from a rendered chip's table. It
// starts at the "## Sources" section and reads the pipe table until the next
// section heading. Non-table lines inside the section (notably the "store: ..."
// locator that precedes the table) are skipped, NOT treated as the end — the
// locator is why a store-bearing chip must still resolve its sources.
func chipSourceIDs(body string) []string {
	var ids []string
	inTable := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			if inTable {
				break
			}
			inTable = strings.HasPrefix(line, "## Sources")
			continue
		}
		if !inTable || !strings.HasPrefix(line, "| ") {
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
