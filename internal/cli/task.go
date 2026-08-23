package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// taskCmd is the broker-lite loop: a store-native pool of claimable work items.
// `add` is the producer (a sentence plus flags, no YAML), `next` is the fleet
// consumer side: an agent pulls only work whose tier floor fits its budget,
// atomically. `run` executes a pulled item end-to-end — generate, execute,
// capture, close. Failure bumps the floor so the item re-enters the pool for a
// more capable agent; past strong it is rejected for a human, never looped.
func taskCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "claimable work items: add/next/run/take/drop/list/done/fail/workflow"}
	cmd.AddCommand(taskAddCmd(), taskNextCmd(), taskRunCmd(), taskTakeCmd(), taskDropCmd(), taskListCmd(), taskDoneCmd(), taskFailCmd(), taskWorkflowCmd())
	return cmd
}

// taskDropCmd is the human's kill switch for blocked work: without it, a
// blocked task haunts every future brief and every session burns time
// re-failing it. Dropping is a decision, recorded in the pool.
func taskDropCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "drop <id>",
		Short: "reject a task by human decision (blocked, obsolete, or out of scope)",
		Example: `  ward task drop task-1a2b
  ward task drop task-1a2b --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			t, err := s.DropTask(args[0])
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(t)
			} else {
				printLine("dropped " + t.ID + " (rejected; will not appear in brief again)")
			}
			return nil
		},
	}
	return c
}

// taskTakeCmd recovers work from a dead session: a claimed task whose agent
// vanished must be takeable, or one crash wedges the item forever. Explicit
// attribution — the new holder is on record.
func taskTakeCmd() *cobra.Command {
	var by string
	c := &cobra.Command{
		Use:   "take <id>",
		Short: "take over a task's claim (recover from a dead session, or acquire an open one)",
		Example: `  ward task take task-1a2b --by architect
  ward task take task-1a2b --by ox-alpha --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			if by == "" {
				return failErr(fmt.Errorf("--by <agent-name> is required"))
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			t, err := s.TakeTask(args[0], by)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(t)
			} else {
				printTask(t)
				printLine("implement the title FIRST, then prove it: ward task run " + t.ID)
			}
			return nil
		},
	}
	c.Flags().StringVar(&by, "by", "", "agent name taking the claim (required)")
	return c
}

func taskAddCmd() *cobra.Command {
	var kind, tier, verifyCmd, run, tags string
	c := &cobra.Command{
		Use:   "add <title>",
		Short: "create a claimable work item (no YAML required)",
		Example: `  ward task add "fix login redirect" --tier mid --run "go test ./..."
  ward task add "write auth spec" --kind test --verify-cmd "test -s .spec/auth.md" --tags topic:auth
  ward task add "tiny cleanup" --tier cheap --json`,
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
			// Field-report guard (muse-spark DX report): agents new to ward
			// used placeholder runs (`true`) that never exercised real code,
			// so tasks closed as done while proving nothing. Warn loudly at
			// authoring time; stderr keeps --json output parseable.
			warn := func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) }
			switch {
			case run == "" && verifyCmd == "":
				warn("task has NO acceptance check: pass --run/--verify-cmd exercising the real change, or completion proves nothing (phantom success)")
			case strings.TrimSpace(run) == "true", strings.TrimSpace(verifyCmd) == "true":
				warn("placeholder check 'true' closes this task while proving nothing: make the gate exercise the real change")
			}
			id, err := s.CreateTask(store.Task{
				Title: title, Kind: kind, TierFloor: tier,
				VerifyCmd: verifyCmd, Run: run, Tags: splitCSV(tags),
			})
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]string{"id": id, "title": title, "tier_floor": orDefault(tier, "mid"), "tags": tags})
			} else {
				printLine(fmt.Sprintf("task %s added (%s floor): %s", id, orDefault(tier, "mid"), title))
			}
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "default", "node kind (default|test|approval)")
	c.Flags().StringVar(&tier, "tier", "mid", "minimum capable tier = admission floor (cheap|mid|strong)")
	c.Flags().StringVar(&verifyCmd, "verify-cmd", "", "extra verification command, AND-chained into the acceptance gate")
	c.Flags().StringVar(&run, "run", "", "command that performs the work AND gates completion (the acceptance check - make it exercise the real change)")
	c.Flags().StringVar(&tags, "tags", "", "topic tags (comma-separated) that let verified results compound across tasks sharing them")
	return c
}

func taskNextCmd() *cobra.Command {
	var by, maxTier string
	c := &cobra.Command{
		Use:   "next",
		Short: "atomically pull the highest-floor open task your budget admits",
		Example: `  ward task next --by agent-3 --max-tier mid
  ward task next --by atlas --max-tier strong --json`,
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
				// Implement-first ordering stated explicitly: engineers that
				// ran the check before the work existed burned their whole
				// escalation budget on trivially-failing gates.
				printLine("implement the title FIRST, then prove it: ward task run " + t.ID)
			}
			return nil
		},
	}
	c.Flags().StringVar(&by, "by", "", "agent name taking the work (required)")
	c.Flags().StringVar(&maxTier, "max-tier", "strong", "this agent's budget ceiling; never offered work above it (cheap|mid|strong)")
	return c
}

// taskRunCmd is the execution bridge: a pulled item becomes finished work in
// ONE command. It generates the single-node workflow, runs it through the
// engine (routing, live verify, escalation), auto-captures the result on
// success, and closes the task. On engine failure it releases the task back
// into the pool one tier higher (FailTask); on rejection past strong it stops
// for a human. The agent never threads ids between commands or retries by hand.
func taskRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run <id>",
		Short: "execute a claimed task end-to-end: generate workflow, run, capture, close",
		Example: `  ward task run task-1a2b
  ward task run task-1a2b --json`,
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
			if t.Status == "open" {
				return failErr(fmt.Errorf("task %s is not claimed: pull it first with ward task next --by <your-name>", t.ID))
			}
			if t.Status != "claimed" {
				return failErr(fmt.Errorf("task %s is %s, not executable", t.ID, t.Status))
			}

			path := "workflows/task-" + strings.TrimPrefix(t.ID, "task-") + ".yaml"
			wf := orchestration.TaskWorkflow(t.ID, t.Title, t.Kind, t.Run, t.VerifyCmd, t.Tags)
			if err := wf.Save(path); err != nil {
				return failErr(err)
			}
			if err := s.SetTaskWorkflow(t.ID, path); err != nil {
				return failErr(err)
			}

			eng := &orchestration.Engine{Store: s, AutoApprove: true}
			runID, err := eng.StartWorkflow(wf)
			if err != nil {
				return failErr(err)
			}
			nCaptured := autoCapture(s, wf, runID)
			r, err := s.LoadRun(runID)
			if err != nil {
				return failErr(err)
			}

			out := map[string]string{"task": t.ID, "run": runID, "run_status": r.Status}
			switch r.Status {
			case "completed":
				if err := s.CompleteTask(t.ID, t.ClaimedBy); err != nil {
					return failErr(err)
				}
				out["task_status"] = "done"
				out["captured"] = fmt.Sprintf("%d", nCaptured)
			case "rejected":
				ft, err := s.FailTask(t.ID)
				if err != nil {
					return failErr(err)
				}
				out["task_status"] = ft.Status
				out["tier_floor"] = ft.TierFloor
				if ft.Status == "rejected" {
					out["dossier"] = "ward reject " + runID
				}
			default:
				// awaiting_approval or paused: keep the claim, surface next step.
				out["task_status"] = "claimed"
				out["resume"] = "ward run resume " + runID + " --auto-approve"
			}
			if jsonOut {
				printJSON(out)
			} else {
				printLine(fmt.Sprintf("task %s: run %s -> %s", t.ID, runID, r.Status))
				switch out["task_status"] {
				case "done":
					if nCaptured > 0 {
						printLine(fmt.Sprintf("task closed as done; %d result(s) captured for the next session", nCaptured))
					} else {
						printLine("task closed as done; NOTHING captured (this task has no runnable check - add --run/--verify-cmd so results can be recorded)")
					}
				case "open":
					printLine("task re-entered pool at floor " + out["tier_floor"])
					printLine("to continue THIS work: ward task take " + t.ID + " --by <your-name> ; then ward task run " + t.ID)
					printLine("otherwise pick different work: ward task next")
				case "rejected":
					printLine("escalation budget spent — needs a human; dossier: " + out["dossier"])
				default:
					printLine("task stays claimed; resume: " + out["resume"])
				}
			}
			return nil
		},
	}
	return c
}

func taskListCmd() *cobra.Command {
	var status string
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "list tasks in the pool",
		Example: `  ward task list
  ward task list --status open -n 20
  ward task list --status done --json`,
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
			if ts == nil {
				ts = []store.Task{}
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
	c.Flags().IntVarP(&limit, "limit", "n", 50, "max tasks")
	return c
}

func taskDoneCmd() *cobra.Command {
	var by string
	c := &cobra.Command{
		Use:   "done <id>",
		Short: "mark a claimed task completed",
		Example: `  ward task done task-1a2b --by agent-3
  ward task done task-1a2b --json`,
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
		Example: `  ward task fail task-1a2b
  ward task fail task-1a2b --json`,
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
		Example: `  ward task workflow task-1a2b
  ward task workflow task-1a2b --out workflows/auth.yaml --json`,
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
			wf := orchestration.TaskWorkflow(t.ID, t.Title, t.Kind, t.Run, t.VerifyCmd, t.Tags)
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
