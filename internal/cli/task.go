package cli

import (
	"fmt"
	"strings"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// taskCmd is the broker-lite loop: a store-native pool of claimable work items.
// `add` is the producer (a sentence plus flags, no YAML), `next` is the fleet
// consumer side: an agent pulls only work whose tier floor fits its budget,
// atomically. Failure bumps the floor so the item re-enters the pool for a
// more capable agent; past strong it is rejected for a human, never looped.
func taskCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "claimable work items: add/next/list/done/fail/workflow"}
	cmd.AddCommand(taskAddCmd(), taskNextCmd(), taskListCmd(), taskDoneCmd(), taskFailCmd(), taskWorkflowCmd())
	return cmd
}

func taskAddCmd() *cobra.Command {
	var kind, tier, verifyCmd, run string
	c := &cobra.Command{
		Use:   "add <title>",
		Short: "create a claimable work item (no YAML required)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("task needs a title"))
			}
			title := strings.Join(args, " ")
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			if tier != "" && tier != "cheap" && tier != "mid" && tier != "strong" {
				return failErr(fmt.Errorf("invalid --tier %q (cheap|mid|strong)", tier))
			}
			id, err := s.CreateTask(store.Task{
				Title: title, Kind: kind, TierFloor: tier,
				VerifyCmd: verifyCmd, Run: run,
			})
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]string{"id": id, "title": title, "tier_floor": orDefault(tier, "mid")})
			} else {
				printLine(fmt.Sprintf("task %s added (%s floor): %s", id, orDefault(tier, "mid"), title))
			}
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "default", "node kind (default|test|approval)")
	c.Flags().StringVar(&tier, "tier", "mid", "minimum capable tier = admission floor (cheap|mid|strong)")
	c.Flags().StringVar(&verifyCmd, "verify-cmd", "", "verification command for the work")
	c.Flags().StringVar(&run, "run", "", "command that performs the work")
	return c
}

func taskNextCmd() *cobra.Command {
	var by, maxTier string
	c := &cobra.Command{
		Use:   "next",
		Short: "atomically pull the highest-floor open task your budget admits",
		RunE: func(cmd *cobra.Command, args []string) error {
			if by == "" {
				return failErr(fmt.Errorf("--by <agent-name> is required (claims must be attributable)"))
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			t, ok, err := s.ClaimNextTask(by, maxTier)
			if err != nil {
				return failErr(err)
			}
			if !ok {
				if jsonOut {
					printJSON(map[string]any{"task": nil})
				} else {
					printLine("no open tasks within budget " + orDefault(maxTier, "strong"))
				}
				return nil
			}
			if jsonOut {
				printJSON(t)
			} else {
				printTask(t)
				printLine("suggested next steps:")
				printLine("  ward task workflow " + t.ID + "   # generate a runnable single-node workflow")
				printLine("  ward run start --workflow <that file>")
				printLine("  on success: ward task done " + t.ID + "; on failure: ward task fail " + t.ID)
			}
			return nil
		},
	}
	c.Flags().StringVar(&by, "by", "", "agent name taking the work (required)")
	c.Flags().StringVar(&maxTier, "max-tier", "strong", "this agent's budget ceiling; never offered work above it (cheap|mid|strong)")
	return c
}

func taskListCmd() *cobra.Command {
	var status string
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "list tasks in the pool",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			ts, err := s.ListTasks(status, limit)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(ts)
				return nil
			}
			if len(ts) == 0 {
				printLine("no tasks" + orDefault(status, ""))
			}
			for _, t := range ts {
				printTask(t)
			}
			return nil
		},
	}
	c.Flags().StringVar(&status, "status", "", "open|claimed|done|rejected")
	c.Flags().IntVar(&limit, "limit", 50, "max tasks")
	return c
}

func taskDoneCmd() *cobra.Command {
	var by string
	c := &cobra.Command{
		Use:   "done <id>",
		Short: "mark a claimed task completed",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			if err := s.CompleteTask(args[0], by); err != nil {
				return failErr(err)
			}
			out := map[string]string{"id": args[0], "status": "done"}
			if jsonOut {
				printJSON(out)
			} else {
				printLine("task " + args[0] + " done")
			}
			return nil
		},
	}
	c.Flags().StringVar(&by, "by", "agent", "completing agent")
	return c
}

func taskFailCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "fail <id>",
		Short: "release a claimed task back to the pool at one tier higher (rejected past strong)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			t, err := s.FailTask(args[0])
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(t)
			} else {
				printLine(fmt.Sprintf("task %s -> %s (floor now %s, escalation %d)",
					t.ID, t.Status, t.TierFloor, t.Escalation))
				if t.Status == "rejected" {
					printLine("escalation budget exhausted: needs a human")
				}
			}
			return nil
		},
	}
	return c
}

// taskWorkflowCmd generates a runnable single-node workflow from a task, so an
// agent can go from pulled item to executed DAG without hand-writing YAML.
func taskWorkflowCmd() *cobra.Command {
	var out string
	c := &cobra.Command{
		Use:   "workflow <id>",
		Short: "generate a runnable single-node workflow for a claimed task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			t, err := s.GetTask(args[0])
			if err != nil {
				return failErr(err)
			}
			path := out
			if path == "" {
				path = "workflows/task-" + strings.TrimPrefix(t.ID, "task-") + ".yaml"
			}
			wf := orchestration.TaskWorkflow(t.ID, t.Title, t.Kind, t.Run, t.VerifyCmd)
			if err := wf.Save(path); err != nil {
				return failErr(err)
			}
			if err := s.SetTaskWorkflow(t.ID, path); err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]string{"id": t.ID, "workflow": path})
			} else {
				printLine("wrote " + path)
				printLine("run it: ward run start --workflow " + path)
			}
			return nil
		},
	}
	c.Flags().StringVar(&out, "out", "", "output path (default workflows/task-<id>.yaml)")
	return c
}

func printTask(t store.Task) {
	line := fmt.Sprintf("%s [%s] floor=%s esc=%d %s", t.ID, t.Status, t.TierFloor, t.Escalation, t.Title)
	if t.ClaimedBy != "" {
		line += fmt.Sprintf(" (by %s)", t.ClaimedBy)
	}
	printLine(line)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
