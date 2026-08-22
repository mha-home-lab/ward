package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// memoryContextCmd assembles a compact, injection-ready context block from a
// search: ids, kind, summary, tags, and verify_status — but NOT full content.
// This is chef's compact context block, so an agent can be pointed at relevant
// prior knowledge without dumping whole artifacts.
func memoryContextCmd() *cobra.Command {
	var project string
	c := &cobra.Command{
		Use:   "context <query>",
		Short: "assemble a compact memory context block (no full content) for injection",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("context needs a query"))
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			res, err := s.SearchArtifacts(args[0], "", project, 20)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				out := make([]map[string]string, 0, len(res))
				for _, a := range res {
					out = append(out, map[string]string{
						"id": a.ID, "kind": a.Kind, "summary": a.Summary,
						"tags": strings.Join(a.Tags, ","), "verify_status": a.VerifyStatus,
					})
				}
				printJSON(out)
			} else {
				if len(res) == 0 {
					printLine("no context for " + args[0])
				}
				for _, a := range res {
					printLine(fmt.Sprintf("[%s] %s %s #%s verify=%s", a.ID, a.Kind, a.Summary, strings.Join(a.Tags, ","), a.VerifyStatus))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "project lens")
	return c
}

// memoryStaleCmd surfaces problem artifacts: those whose live verify_status is
// stale/error/unknown, plus (with --days) accepted artifacts that are rarely
// reused. --mark sets an artifact's verify_status to stale by hand.
func memoryStaleCmd() *cobra.Command {
	var days int
	var markID string
	c := &cobra.Command{
		Use:   "stale",
		Short: "surface stale/error/unknown artifacts (and rarely-used ones with --days)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			if markID != "" {
				if err := s.SetVerify(markID, "stale"); err != nil {
					return failErr(err)
				}
				printLine("marked " + markID + " stale")
				return nil
			}
			all, err := s.ListArtifacts("", "", "", 1000)
			if err != nil {
				return failErr(err)
			}
			var stale []store.Artifact
			for _, a := range all {
				if a.VerifyStatus == "stale" || a.VerifyStatus == "error" || a.VerifyStatus == "unknown" {
					stale = append(stale, a)
				}
			}
			if days > 0 {
				rare, _ := s.StaleArtifacts(days, 100)
				stale = append(stale, rare...)
			}
			if jsonOut {
				out := make([]map[string]string, 0, len(stale))
				for _, a := range stale {
					out = append(out, map[string]string{"id": a.ID, "verify_status": a.VerifyStatus, "summary": a.Summary})
				}
				printJSON(out)
			} else {
				if len(stale) == 0 {
					printLine("no stale artifacts")
				}
				for _, a := range stale {
					printLine(fmt.Sprintf("%s verify=%s %s", a.ID, a.VerifyStatus, a.Summary))
				}
			}
			return nil
		},
	}
	c.Flags().IntVar(&days, "days", 0, "also include accepted artifacts unused for N days")
	c.Flags().StringVar(&markID, "mark", "", "mark an artifact stale by id")
	return c
}

// memoryClaimCmd is advisory topic coordination (chef's coordination-001):
// agents voluntarily reserve a topic so two sessions don't both do the work.
// It is advisory only — there is no locking; overlap is warned (or errored with
// --strict) but the write still proceeds unless --strict.
func memoryClaimCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "claim", Short: "advisory topic claim (coordination; no locking)"}
	cmd.AddCommand(claimAddCmd(), claimReleaseCmd(), claimListCmd())
	return cmd
}

func claimAddCmd() *cobra.Command {
	var by, project string
	var ttl int
	var strict bool
	c := &cobra.Command{
		Use:   "add <topic>",
		Short: "reserve a topic (advisory); warns on overlap, errors with --strict",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("claim add needs a topic"))
			}
			topic := args[0]
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			overlap := activeClaims(s, topic, project)
			if len(overlap) > 0 {
				msg := fmt.Sprintf("claim overlap on %q: %v", topic, overlap)
				if strict {
					return failErr(fmt.Errorf("%s", msg))
				}
				printLine("WARN: " + msg)
			}

			expires := ""
			if ttl > 0 {
				expires = time.Now().UTC().Add(time.Duration(ttl) * time.Minute).Format("2006-01-02T15:04:05Z")
			}
			a := store.Artifact{
				Kind: "claim", Summary: topic,
				Content:   fmt.Sprintf("agent=%s expires=%s", by, expires),
				Tags:      []string{"claim", topic, project},
				Status:    "accepted",
				CreatedBy: by,
				Ceremony:  "light",
				Local:     true,
			}
			id, err := s.UpsertArtifact(a)
			if err != nil {
				return failErr(err)
			}
			if expires != "" {
				_ = s.SetExpires(id, expires)
			}
			printLine(fmt.Sprintf("claimed %q -> %s (by %s)", topic, id, by))
			return nil
		},
	}
	c.Flags().StringVar(&by, "by", "agent", "claiming agent")
	c.Flags().StringVar(&project, "project", "", "project lens")
	c.Flags().IntVar(&ttl, "ttl", 0, "claim TTL in minutes (0 = no expiry)")
	c.Flags().BoolVar(&strict, "strict", false, "error instead of warn on overlap")
	return c
}

func claimReleaseCmd() *cobra.Command {
	var project string
	c := &cobra.Command{
		Use:   "release <topic>",
		Short: "release an active claim on a topic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("claim release needs a topic"))
			}
			topic := args[0]
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			released := activeClaims(s, topic, project)
			for _, id := range released {
				_ = s.Supersede(id, "", "claim released")
			}
			if len(released) == 0 {
				printLine("no active claim on " + topic)
			} else {
				printLine("released: " + strings.Join(released, " "))
			}
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "project lens")
	return c
}

func claimListCmd() *cobra.Command {
	var project string
	c := &cobra.Command{
		Use:   "list",
		Short: "list active claims",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			var active []store.Artifact
			for _, a := range activeClaims(s, "", project) {
				if art, err := s.GetArtifact(a); err == nil {
					active = append(active, art)
				}
			}
			if jsonOut {
				out := make([]map[string]string, 0, len(active))
				for _, a := range active {
					out = append(out, map[string]string{"id": a.ID, "topic": claimTopic(a), "by": a.CreatedBy, "expires": a.ExpiresAt})
				}
				printJSON(out)
			} else {
				if len(active) == 0 {
					printLine("no active claims")
				}
				for _, a := range active {
					printLine(fmt.Sprintf("%s topic=%s by=%s expires=%s", a.ID, claimTopic(a), a.CreatedBy, a.ExpiresAt))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "project lens")
	return c
}

// activeClaims returns ids of accepted, non-expired claims matching topic
// (empty = any) and project (empty = any).
func activeClaims(s *store.Store, topic, project string) []string {
	all, err := s.ListArtifacts("accepted", "claim", project, 100)
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range all {
		if claimExpired(a) {
			continue
		}
		if topic != "" && claimTopic(a) != topic {
			continue
		}
		out = append(out, a.ID)
	}
	return out
}

// claimTopic is the topic tag (the second tag: ["claim", topic, project]).
func claimTopic(a store.Artifact) string {
	if len(a.Tags) >= 2 {
		return a.Tags[1]
	}
	return a.Summary
}

func claimExpired(a store.Artifact) bool {
	if a.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", a.ExpiresAt)
	if err != nil {
		return false
	}
	return time.Now().UTC().After(t)
}
