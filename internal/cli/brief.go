package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// briefCmd is the session bootstrap: the ONE command an agent runs at the start
// of work. It performs the tick sweep (live re-verification + expired-claim
// cleanup), then assembles everything the agent needs to act without asking a
// human or re-reading docs: relevant prior knowledge, open runs, active claims,
// store health, and concrete next actions. It is the "best friend" handshake —
// state of the world first, opinions second.
func briefCmd() *cobra.Command {
	var repo string
	var limit int
	var compact bool
	c := &cobra.Command{
		Use:   "brief [topic]",
		Short: "session bootstrap: verify, sweep, and report what matters right now",
		Long: "Run this before planning anything. ward brief re-verifies store-local\n" +
			"results live, frees expired claims, then reports verified prior knowledge\n" +
			"for [topic] (if given), open runs, active claims, and suggested next\n" +
			"actions. Only verified artifacts are facts; everything else is a miss.",
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := ""
			if len(args) > 0 {
				topic = strings.Join(args, " ")
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			b := brief{}
			b.Topic = topic
			// Self-identification: an agent reading this output must be able to
			// detect a stale binary or a wrong store without trusting anything
			// else (found the hard way: agents hunt PATH and find old builds).
			b.Version = Version
			b.Store = s.Home

			// 1. Live sweep: drift caught now is a wrong route prevented later.
			var changes []verifyChange
			b.Checked, b.Drift, changes, err = sweepVerify(s, repo)
			if err != nil {
				return failErr(err)
			}
			b.Changes = make([]map[string]string, 0, len(changes))
			for _, ch := range changes {
				b.Changes = append(b.Changes, map[string]string{
					"id": ch.ID, "before": ch.Before, "after": ch.After, "detail": ch.Detail,
				})
			}

			// 2. Free expired reservations so a dead session can't block work.
			swept, err := s.SweepExpiredClaims()
			if err != nil {
				return failErr(err)
			}
			b.ClaimsExpired = swept

			// 3. Prior knowledge for the topic (compact: ids + status only).
			hits, err := contextHits(s, topic, limit)
			if err != nil {
				return failErr(err)
			}
			b.Knowledge = hits

			// 4. Open runs — unfinished business from any prior session.
			runs, err := s.OpenRuns()
			if err != nil {
				return failErr(err)
			}
			b.OpenRuns = make([]map[string]string, 0, len(runs))
			for _, r := range runs {
				b.OpenRuns = append(b.OpenRuns, map[string]string{
					"id": r.ID, "workflow": r.WorkflowName, "status": r.Status, "waiting": r.WaitingApproval,
				})
			}

			// 5. Active claims — topics another agent holds exclusively.
			claimIDs, _ := s.ActiveClaimIDs("", "")
			b.Claims = make([]map[string]string, 0, len(claimIDs))
			for _, id := range claimIDs {
				a, err := s.GetArtifact(id)
				if err != nil {
					continue
				}
				b.Claims = append(b.Claims, map[string]string{
					"id": id, "topic": claimTopic(a), "by": a.CreatedBy, "expires": a.ExpiresAt,
				})
			}

			// 6. The dispatch pool — open work an agent can pull right now.
			openTasks, err := s.ListTasks("open", 100)
			if err != nil {
				return failErr(err)
			}
			b.OpenTasks = make([]map[string]string, 0, len(openTasks))
			for _, t := range openTasks {
				b.OpenTasks = append(b.OpenTasks, map[string]string{
					"id": t.ID, "title": t.Title, "tier_floor": t.TierFloor,
				})
			}

			// 7. Health snapshot.
			accepted, _ := s.ListArtifacts("accepted", "", "", 1000)
			proposed, _ := s.ListArtifacts("proposed", "", "", 1000)
			verified := 0
			for _, a := range accepted {
				if a.VerifyStatus == "verified" {
					verified++
				}
			}
			b.Health = map[string]int{
				"accepted": len(accepted), "verified": verified, "proposed": len(proposed),
			}

			// R5 reflection item: chips are caches of the brain; if their
			// sources drifted, agents are being taught stale facts. Surface
			// it at session start where it can actually change behavior.
			if staleChips := staleChipReport(); len(staleChips) > 0 {
				b.StaleChips = staleChips
				b.Next = append([]string{"STALE CHIPS detected: recompile with ward skill pack <topic>"}, b.Next...)
			}

			// Dead-agent signal (L10): claims aging past 30 minutes with no
			// closure are either long work or a dead session - high signal,
			// cheap to check.
			stale, err := s.StaleClaims(30)
			if err == nil && len(stale) > 0 {
				_ = stale
				b.StaleClaims = make([]map[string]string, 0, len(stale))
				for _, t := range stale {
					b.StaleClaims = append(b.StaleClaims, map[string]string{
						"id": t.ID, "by": orDefault(t.ClaimedBy, "?"), "mins_aged": "30+",
						"title": t.Title,
					})
					b.Next = append(b.Next,
						fmt.Sprintf("STALE CLAIM %s held by %s since >30m: verify holder alive; recover with ward task take %s --by <you>",
							t.ID, orDefault(t.ClaimedBy, "?"), t.ID))
				}
			}

			b.Next = nextActions(b)
			if compact {
				// Budget-aware mode (rd:c2 a5fee2fa): small models choke on
				// unbounded context; trim summaries and drop the health block.
				for i := range b.Knowledge {
					r := []rune(b.Knowledge[i].Summary)
					if len(r) > 80 {
						b.Knowledge[i].Summary = string(r[:80]) + "..."
					}
				}
				for i := range b.OpenTasks {
					r := []rune(b.OpenTasks[i]["title"])
					if len(r) > 60 {
						b.OpenTasks[i]["title"] = string(r[:60]) + "..."
					}
				}
				b.Health = map[string]int{}
			}

			if jsonOut {
				printJSON(b)
			} else {
				printHumanBrief(b)
			}
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "repo root for verification")
	c.Flags().IntVar(&limit, "limit", 5, "max knowledge hits to show")
	c.Flags().BoolVar(&compact, "compact", false, "token-budgeted output: trimmed summaries, no health block")
	return c
}

// brief is the structured bootstrap report (--json shape).
type brief struct {
	Version       string              `json:"version"`
	Store         string              `json:"store"`
	Topic         string              `json:"topic"`
	Checked       int                 `json:"reverified"`
	Drift         int                 `json:"drift"`
	ClaimsExpired int64               `json:"claims_expired"`
	Changes       []map[string]string `json:"changes,omitempty"`
	Knowledge     []knowledgeHit      `json:"knowledge,omitempty"`
	OpenRuns      []map[string]string `json:"open_runs,omitempty"`
	Claims        []map[string]string `json:"active_claims,omitempty"`
	OpenTasks     []map[string]string `json:"open_tasks,omitempty"`
	Health        map[string]int      `json:"health"`
	StaleChips    []string            `json:"stale_chips,omitempty"`
	StaleClaims   []map[string]string `json:"stale_claims,omitempty"`
	Next          []string            `json:"next"`
}

// knowledgeHit is one compact prior-knowledge pointer (never full content).
type knowledgeHit struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	Tags    string `json:"tags"`
	Verify  string `json:"verify_status"`
}

func contextHits(s *store.Store, topic string, limit int) ([]knowledgeHit, error) {
	if topic == "" {
		return nil, nil
	}
	res, err := s.SearchArtifacts(topic, "", "", limit)
	if err != nil {
		return nil, err
	}
	out := make([]knowledgeHit, 0, len(res))
	for _, a := range res {
		out = append(out, knowledgeHit{
			ID: a.ID, Kind: a.Kind, Summary: a.Summary,
			Tags: strings.Join(a.Tags, ","), Verify: a.VerifyStatus,
		})
	}
	return out, nil
}

// nextActions turns the observed state into imperative guidance — the part that
// keeps an agent from guessing what to do first.
func nextActions(b brief) []string {
	var next []string
	for _, r := range b.OpenRuns {
		switch r["status"] {
		case "awaiting_approval":
			next = append(next, fmt.Sprintf("ward run approve %s %s", r["id"], r["waiting"]))
		case "running":
			next = append(next, fmt.Sprintf("ward run resume %s --auto-approve", r["id"]))
		}
	}
	if b.Drift > 0 {
		next = append(next, fmt.Sprintf("%d previously-verified artifact(s) went STALE: treat them as misses, do not trust their summaries", b.Drift))
	}
	if len(b.OpenTasks) > 0 {
		next = append(next, fmt.Sprintf("%d open task(s) in the pool: ward task next --by <your-name> --max-tier <budget>", len(b.OpenTasks)))
	}
	if b.Topic != "" {
		verified, unverified := 0, 0
		for _, k := range b.Knowledge {
			if k.Verify == "verified" {
				verified++
			} else {
				unverified++
			}
		}
		switch {
		case verified > 0:
			next = append(next, fmt.Sprintf("reuse verified context (%d hit(s)) instead of re-solving; fetch full content with: ward memory get <id>", verified))
		case unverified > 0:
			next = append(next, "candidates exist but NONE are live-verified: do not trust them, do the work at full attention")
		default:
			next = append(next, "no prior knowledge for this topic: nothing to reuse, work at full attention")
		}
	}
	for _, cl := range b.Claims {
		next = append(next, fmt.Sprintf("topic %q is claimed by %s (expires %s): pick different work or wait", cl["topic"], cl["by"], cl["expires"]))
	}
	if len(next) == 0 && b.Health["proposed"] > 0 {
		next = append(next, fmt.Sprintf("%d proposed artifact(s) await review: ward memory list --status proposed", b.Health["proposed"]))
	}
	if len(next) == 0 {
		next = append(next, "store clean: no open runs, no conflicts, nothing stale — proceed with the task")
	}
	return next
}

func printHumanBrief(b brief) {
	fmt.Println("== ward brief ==")
	fmt.Printf("ward %s | store: %s\n", b.Version, b.Store)
	fmt.Printf("swept: re-verified=%d drift=%d expired-claims-freed=%d\n", b.Checked, b.Drift, b.ClaimsExpired)
	for _, ch := range b.Changes {
		fmt.Printf("  drift %s: %s -> %s (%s)\n", ch["id"], ch["before"], ch["after"], ch["detail"])
	}
	if b.Topic != "" {
		fmt.Printf("knowledge[%s]:\n", b.Topic)
		if len(b.Knowledge) == 0 {
			fmt.Println("  (none)")
		}
		for _, k := range b.Knowledge {
			fmt.Printf("  [%s] %s %s #%s verify=%s\n", k.ID, k.Kind, k.Summary, k.Tags, k.Verify)
		}
	}
	if len(b.OpenRuns) > 0 {
		fmt.Println("open runs:")
		for _, r := range b.OpenRuns {
			fmt.Printf("  %s %s (%s) waiting=%s\n", r["id"], r["workflow"], r["status"], r["waiting"])
		}
	}
	if len(b.OpenTasks) > 0 {
		fmt.Println("task pool (open):")
		for _, t := range b.OpenTasks {
			fmt.Printf("  %s floor=%s %s\n", t["id"], t["tier_floor"], t["title"])
		}
	}
	for _, sc := range b.StaleChips {
		fmt.Println("STALE CHIP: " + sc)
	}
	for _, scl := range b.StaleClaims {
		fmt.Printf("STALE CLAIM: %s held by %s (%s)\n", scl["id"], scl["by"], scl["title"])
	}
	if len(b.Claims) > 0 {
		fmt.Println("active claims:")
		for _, cl := range b.Claims {
			fmt.Printf("  topic=%s by=%s expires=%s\n", cl["topic"], cl["by"], cl["expires"])
		}
	}
	if b.Health != nil {
		fmt.Printf("store: accepted=%d verified=%d proposed=%d\n",
			b.Health["accepted"], b.Health["verified"], b.Health["proposed"])
	}
	fmt.Println("next:")
	for i, n := range b.Next {
		fmt.Printf("  %d. %s\n", i+1, n)
	}
}

// staleChipReport scans .opencode/skills/*/SKILL.md and reports chips whose
// source artifacts drifted or vanished.
func staleChipReport() []string {
	matches, err := filepath.Glob(filepath.Join(".opencode", "skills", "*", "SKILL.md"))
	if err != nil {
		return nil
	}
	s, err := store.Open()
	if err != nil {
		return nil
	}
	defer s.DB.Close()
	var stale []string
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		chip := strings.TrimSuffix(filepath.Base(filepath.Dir(path)), "")
		for _, id := range chipSourceIDs(string(data)) {
			a, err := s.GetArtifact(id)
			if err != nil || a.Status == "superseded" || a.VerifyStatus == "stale" || a.VerifyStatus == "error" {
				stale = append(stale, fmt.Sprintf("%s (source %s drifted)", chip, id))
				break
			}
		}
	}
	sort.Strings(stale)
	return stale
}
