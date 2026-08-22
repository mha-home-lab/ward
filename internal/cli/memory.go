package cli

import (
	"fmt"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/verification"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "initialize the ward store (sqlite + schema)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			if jsonOut {
				printJSON(map[string]string{"status": "ok", "home": s.Home})
			} else {
				printLine("ward initialized at " + s.Home)
			}
			return nil
		},
	}
}

func memoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "agent memory store (put/get/search/list/promote/supersede/handoff)"}
	cmd.AddCommand(memoryPutCmd(), memoryGetCmd(), memorySearchCmd(), memoryListCmd(), memoryPromoteCmd(), memorySupersedeCmd(), memoryHandoffCmd())
	return cmd
}

func memoryPutCmd() *cobra.Command {
	var kind, summary, content, tags, verifyCmd, verifyKind, project, by, ceremony string
	var imported bool
	c := &cobra.Command{
		Use:   "put",
		Short: "store a memory artifact (light ceremony auto-accepts)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			if summary == "" {
				return failErr(errMissingSummary)
			}
			if kind == "" {
				kind = "context"
			}
			if ceremony == "" {
				ceremony = "light"
			}
			a := store.Artifact{
				Kind: kind, Summary: summary, Content: content,
				Tags:  splitCSV(tags), Status: "proposed",
				CreatedBy: by, Project: project,
				VerifyCmd: verifyCmd, VerifyKind: verifyKind, Local: !imported, Ceremony: ceremony,
			}
			id, err := s.UpsertArtifact(a)
			if err != nil {
				return failErr(err)
			}
			// Light ceremony auto-accepts (spec). Auto-accepted but unverified
			// artifacts still cannot vote cheap until live-verified.
			if ceremony == "light" {
				_, _ = s.Promote([]string{id}, "auto-accept (light ceremony)", by)
			}
			if jsonOut {
				printJSON(map[string]string{"id": id, "status": "accepted", "ceremony": ceremony})
			} else {
				printLine("stored " + id + " (accepted, " + ceremony + ")")
			}
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "artifact kind (context|solution|feedback|spec|config|error)")
	c.Flags().StringVar(&summary, "summary", "", "short summary (required)")
	c.Flags().StringVar(&content, "content", "", "artifact content")
	c.Flags().StringVar(&tags, "tags", "", "comma-separated tags")
	c.Flags().StringVar(&verifyCmd, "verify-cmd", "", "verification command")
	c.Flags().StringVar(&verifyKind, "verify-kind", "", "shell|grep|build|test|hash")
	c.Flags().StringVar(&project, "project", "", "project namespace")
	c.Flags().StringVar(&by, "by", "agent", "creator name")
	c.Flags().StringVar(&ceremony, "ceremony", "light", "light (auto-accept) | full")
	c.Flags().BoolVar(&imported, "imported", false, "mark as imported (not store-local; verify not executed)")
	return c
}

func memoryGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "show one artifact by id",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			a, err := s.GetArtifact(args[0])
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(a)
			} else {
				printLine(fmt.Sprintf("[%s] %s %s", a.ID, a.Kind, a.Summary))
				printLine(fmt.Sprintf("  status=%s verify=%s local=%v tags=%v", a.Status, a.VerifyStatus, a.Local, a.Tags))
				printLine("  " + a.Content)
			}
			return nil
		},
	}
}

func memorySupersedeCmd() *cobra.Command {
	var with, reason string
	c := &cobra.Command{
		Use:   "supersede <id>",
		Short: "mark an artifact superseded (optionally by a successor id)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			if reason == "" {
				reason = "superseded"
			}
			if err := s.Supersede(args[0], with, reason); err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]string{"id": args[0], "superseded_by": with, "status": "ok"})
			} else {
				printLine("superseded " + args[0] + " by " + with)
			}
			return nil
		},
	}
	c.Flags().StringVar(&with, "with", "", "successor artifact id")
	c.Flags().StringVar(&reason, "reason", "", "why superseded")
	return c
}

func memorySearchCmd() *cobra.Command {
	var kind, project string
	var limit int
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "semantic-ish search with term-drop relaxation",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedQuery)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			res, err := s.SearchArtifacts(args[0], kind, project, limit)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(res)
			} else {
				if len(res) == 0 {
					printLine("no matches")
				}
				for _, a := range res {
					printLine(fmt.Sprintf("[%s] %s %s (%s/%s)", a.ID, a.Kind, a.Summary, a.Status, a.VerifyStatus))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "filter by kind")
	c.Flags().StringVar(&project, "project", "", "filter by project")
	c.Flags().IntVar(&limit, "limit", 10, "max results")
	return c
}

func memoryListCmd() *cobra.Command {
	var status, kind, project string
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "list artifacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			res, err := s.ListArtifacts(status, kind, project, limit)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(res)
			} else {
				for _, a := range res {
					printLine(fmt.Sprintf("[%s] %s %s (%s/%s)", a.ID, a.Kind, a.Summary, a.Status, a.VerifyStatus))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&status, "status", "", "proposed|accepted|superseded")
	c.Flags().StringVar(&kind, "kind", "", "filter by kind")
	c.Flags().StringVar(&project, "project", "", "filter by project")
	c.Flags().IntVar(&limit, "limit", 20, "max results")
	return c
}

func memoryPromoteCmd() *cobra.Command {
	var reason, by string
	c := &cobra.Command{
		Use:   "promote <id>...",
		Short: "promote proposed artifacts to accepted",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			out, err := s.Promote(args, reason, by)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(out)
			} else {
				for _, l := range out {
					printLine(l)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&reason, "reason", "reviewed", "promotion reason")
	c.Flags().StringVar(&by, "by", "agent", "reviewer name")
	return c
}

func memoryHandoffCmd() *cobra.Command {
	var incomplete bool
	c := &cobra.Command{
		Use:   "handoff",
		Short: "produce a structured handoff of incomplete work for the next agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			proposed, _ := s.ListArtifacts("proposed", "", "", 50)
			stale, _ := s.StaleArtifacts(30, 50)
			runs, _ := s.DB.Query("SELECT id, workflow_name, status, waiting_approval_id FROM runs WHERE status IN ('awaiting_approval','running')")
			type runView struct {
				ID, Workflow, Status, Waiting string
			}
			var runViews []runView
			if runs != nil {
				defer runs.Close()
				for runs.Next() {
					var rv runView
					var wa string
					_ = runs.Scan(&rv.ID, &rv.Workflow, &rv.Status, &wa)
					rv.Waiting = wa
					runViews = append(runViews, rv)
				}
			}
			var items []map[string]any
			for _, p := range proposed {
				items = append(items, map[string]any{
					"id": p.ID, "summary": p.Summary, "files": p.Tags,
					"why": "proposed (not promoted)", "attempted_at": p.CreatedAt,
				})
			}
			for _, st := range stale {
				items = append(items, map[string]any{
					"id": st.ID, "summary": st.Summary, "files": st.Tags,
					"why": "stale (not re-verified)", "attempted_at": st.CreatedAt,
				})
			}
			for _, r := range runViews {
				items = append(items, map[string]any{
					"id": r.ID, "summary": "run " + r.Workflow, "files": []string{},
					"why": "open run: " + r.Status + " waiting=" + r.Waiting, "attempted_at": "",
				})
			}
			handoff := map[string]any{
				"incomplete": incomplete,
				"summary":    fmt.Sprintf("%d proposed, %d stale, %d open runs", len(proposed), len(stale), len(runViews)),
				"items":     items,
			}
			if jsonOut {
				printJSON(handoff)
			} else {
				printLine(fmt.Sprintf("incomplete=%v  %s", incomplete, handoff["summary"]))
				for _, it := range items {
					printLine(fmt.Sprintf("  [%s] %v — %v", it["id"], it["why"], it["summary"]))
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&incomplete, "incomplete", false, "mark handoff as carrying incomplete work")
	return c
}

func verifyCmd() *cobra.Command {
	var repo string
	var trust, all bool
	c := &cobra.Command{
		Use:   "verify <id>",
		Short: "run an artifact's verify_cmd (only for store-local artifacts)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			if all {
				allArt, _ := s.ListArtifacts("", "", "", 1000)
				var out []map[string]string
				for _, a := range allArt {
					if !a.Local {
						continue
					}
					r := verification.Run(a, repo)
					_ = s.SetVerify(a.ID, r.Status)
					out = append(out, map[string]string{"id": a.ID, "status": r.Status, "detail": r.Detail})
				}
				if jsonOut {
					printJSON(out)
				} else {
					for _, o := range out {
						printLine(fmt.Sprintf("%s -> %s: %s", o["id"], o["status"], o["detail"]))
					}
				}
				return nil
			}
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			if trust {
				// --trust promotes an imported artifact to store-local so its
				// verify_cmd may execute (explicit trust boundary crossing).
				if err := s.SetLocal(args[0]); err != nil {
					return failErr(err)
				}
			}
			a, err := s.GetArtifact(args[0])
			if err != nil {
				return failErr(err)
			}
			res := verification.Run(a, repo)
			if err := s.SetVerify(a.ID, res.Status); err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(res)
			} else {
				printLine(fmt.Sprintf("%s -> %s: %s", a.ID, res.Status, res.Detail))
			}
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "repo root for shell/build/test/hash verification")
	c.Flags().BoolVar(&trust, "trust", false, "mark an imported artifact as trusted (local) before verifying")
	c.Flags().BoolVar(&all, "all", false, "verify all store-local artifacts and persist results")
	return c
}

var (
	errMissingSummary = fmt.Errorf("summary is required")
	errNeedQuery      = fmt.Errorf("search needs a query")
	errNeedID         = fmt.Errorf("an artifact id is required")
)

func splitCSV(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
