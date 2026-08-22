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
	cmd := &cobra.Command{Use: "memory", Short: "agent memory store (put/search/list/promote/handoff)"}
	cmd.AddCommand(memoryPutCmd(), memorySearchCmd(), memoryListCmd(), memoryPromoteCmd(), memoryHandoffCmd())
	return cmd
}

func memoryPutCmd() *cobra.Command {
	var kind, summary, content, tags, verifyCmd, verifyKind, project, by string
	var imported bool
	c := &cobra.Command{
		Use:   "put",
		Short: "store a memory artifact",
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
			a := store.Artifact{
				Kind: kind, Summary: summary, Content: content,
				Tags:  splitCSV(tags), Status: "proposed",
				CreatedBy: by, Project: project,
				VerifyCmd: verifyCmd, VerifyKind: verifyKind, Local: !imported, Ceremony: "light",
			}
			id, err := s.UpsertArtifact(a)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]string{"id": id, "status": "proposed"})
			} else {
				printLine("stored " + id + " (proposed)")
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
	c.Flags().BoolVar(&imported, "imported", false, "mark as imported (not store-local; verify not executed)")
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
		Short: "produce a handoff artifact (incomplete work for the next agent)",
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
			if jsonOut {
				printJSON(map[string]any{
					"incomplete":     incomplete,
					"proposed_count": len(proposed),
					"proposed":       proposed,
					"stale_count":    len(stale),
					"stale":          stale,
					"open_runs":      runViews,
				})
			} else {
				printLine(fmt.Sprintf("proposed: %d  stale: %d  open_runs: %d", len(proposed), len(stale), len(runViews)))
				for _, p := range proposed {
					printLine("  proposed: " + p.ID + " " + p.Summary)
				}
				for _, r := range runViews {
					printLine("  run: " + r.ID + " " + r.Status + " waiting=" + r.Waiting)
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
	c := &cobra.Command{
		Use:   "verify <id>",
		Short: "run an artifact's verify_cmd (only for store-local artifacts)",
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
