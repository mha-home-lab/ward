package cli

import (
	"fmt"
	"os"
	"os/exec"
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
	cmd := &cobra.Command{Use: "task", Short: "claimable work items: add/next/run/take/drop/list/done/fail/show/workflow"}
	cmd.AddCommand(taskAddCmd(), taskNextCmd(), taskRunCmd(), taskTakeCmd(), taskDropCmd(), taskListCmd(), taskDoneCmd(), taskFailCmd(), taskShowCmd(), taskWorkflowCmd(), taskCheckpointCmd())
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
			// so tasks closed as done while proving nothing. Phantom gates are
			// HARD-rejected at authoring time (neverphantom): a task must carry
			// a real acceptance check, or the author must consciously accept the
			// "no check" warning for genuinely manual work.
			warn := func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) }
			switch {
			case run == "" && verifyCmd == "":
				warn("task has NO acceptance check: pass --run/--verify-cmd exercising the real change, or completion proves nothing (phantom success)")
			case isTrivialVerify(run):
				return failErr(fmt.Errorf("rejected: --run %q is a phantom gate (true/echo/: prove nothing). Provide a real acceptance check that exercises the change", run))
			case isTrivialVerify(verifyCmd):
				return failErr(fmt.Errorf("rejected: --verify-cmd %q is a phantom gate (true/echo/: prove nothing). Provide a real acceptance check that exercises the change", verifyCmd))
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
				// Mechanical reload: prior knowledge scoped to this task's tags,
				// plus the latest checkpoint — no agent-discipline required.
				printScopedContext(s, t.Tags)
				printLatestCheckpoint(s, t.ID)
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

			// Mechanical reload: surface scoped prior knowledge + the latest
			// checkpoint before the work begins, so the agent can't silently
			// re-derive from scratch three tasks deep.
			if !jsonOut {
				printScopedContext(s, t.Tags)
				printLatestCheckpoint(s, t.ID)
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
			if err := s.SetTaskLastRun(t.ID, runID); err != nil {
				return failErr(err)
			}
			nCaptured := autoCapture(s, wf, runID)
			r, err := s.LoadRun(runID)
			if err != nil {
				return failErr(err)
			}

			out := map[string]string{"task": t.ID, "run": runID, "run_status": r.Status}
			if hits, herr := s.ContextForTask(t.Tags, 5); herr == nil && len(hits) > 0 {
				kk := make([]map[string]string, 0, len(hits))
				for _, a := range hits {
					kk = append(kk, map[string]string{"id": a.ID, "kind": a.Kind, "summary": a.Summary, "verify": a.VerifyStatus})
				}
				out["prior_knowledge"] = fmt.Sprintf("%d artifact(s)", len(kk))
			}
			if cp, cerr := s.LatestCheckpoint(t.ID); cerr == nil && cp != nil {
				out["latest_checkpoint"] = fmt.Sprintf("seq %d: %s", cp.Seq, cp.Summary)
			}
			switch r.Status {
			case "completed":
				// Pre-close gate (transparency patch): a task that declared a
				// gate may only close if the run left verifiable evidence that
				// the check actually ran and exited 0. This is the check that
				// stops a task from closing behind missing/broken proof.
				if t.Run != "" || t.VerifyCmd != "" {
					if err := gateEvidence(t.ID, runID); err != nil {
						return failErr(err)
					}
				}
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
	var force bool
	c := &cobra.Command{
		Use:   "done <id>",
		Short: "mark a claimed task completed",
		Example: `  ward task done task-1a2b --by agent-3
  ward task done task-1a2b --force   # human override when no evidence exists
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
			t, err := s.GetTask(args[0])
			if err != nil {
				return failErr(err)
			}
			// Pre-close gate (transparency patch): if the task declared a gate,
			// 'done' must be backed by a sidecar proving it ran and exited 0 —
			// otherwise 'done' is just a way to bypass the whole transparency
			// guarantee. A human may override with --force, but that override is
			// loudly logged so the bypass is never silent.
			gateFailed := false
			if t.Run != "" || t.VerifyCmd != "" {
				if err := gateEvidence(t.ID, t.LastRunID); err != nil {
					if !force {
						return failErr(err)
					}
					gateFailed = true
					fmt.Fprintln(os.Stderr, "warning: --force overriding missing/!0 verification evidence for task "+t.ID+" (human override, not audit-backed)")
				}
			}
			// --force only records 'force-closed' when it actually overrode a
			// failed/missing gate. If evidence was present and green, --force is a
			// no-op and the task closes as a normal verified 'done' — otherwise the
			// bypass signal would be meaningless and agents could launder real
			// completions through it.
			if force && gateFailed {
				if err := s.ForceCloseTask(args[0], by); err != nil {
					return failErr(err)
				}
				out := map[string]string{"id": args[0], "status": "force-closed", "forced": "true"}
				if jsonOut {
					printJSON(out)
				} else {
					printLine("task " + args[0] + " force-closed (verification evidence bypassed)")
				}
				return nil
			}
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
	c.Flags().BoolVar(&force, "force", false, "override the verification-evidence gate (human decision; logged)")
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

// printScopedContext mechanically injects prior knowledge scoped to the task's
// tags — the reload the protocol used to ask the agent to remember by hand.
func printScopedContext(s *store.Store, tags []string) {
	hits, err := s.ContextForTask(tags, 5)
	if err != nil {
		return
	}
	tagStr := strings.Join(tags, ", ")
	if tagStr == "" {
		tagStr = "(none)"
	}
	printLine("Prior knowledge (scoped to tags: " + tagStr + "):")
	if len(hits) == 0 {
		printLine("  (none)")
		return
	}
	for _, a := range hits {
		printLine(fmt.Sprintf("  - [%s] %s (%s)", a.Kind, a.Summary, a.VerifyStatus))
	}
}

// printLatestCheckpoint surfaces the most recent mid-task offload so the agent
// can trust what it already learned and shed raw exploration.
func printLatestCheckpoint(s *store.Store, taskID string) {
	cp, err := s.LatestCheckpoint(taskID)
	if err != nil || cp == nil {
		return
	}
	printLine(fmt.Sprintf("Latest checkpoint (seq %d):", cp.Seq))
	printLine("  " + cp.Summary)
	if cp.VerifyCmd != "" {
		printLine(fmt.Sprintf("  verify: %s -> exit %d", cp.VerifyCmd, cp.ExitCode))
	}
}

// taskShowCmd is the audit window: it surfaces a task's metadata and the
// evidence of its most recent run (exit code + last 15 lines of the sidecar
// log) so a human or agent can SEE what ran and why — no prying into the binary
// db. This directly answers the "I couldn't inspect task metadata" complaint.
func taskShowCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "audit a task: metadata + last run evidence (exit code, log tail)",
		Example: `  ward task show task-1a2b
  ward task show task-1a2b --json`,
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
			if jsonOut {
				out := map[string]any{
					"id":         t.ID,
					"title":      t.Title,
					"status":     t.Status,
					"kind":       t.Kind,
					"tier_floor": t.TierFloor,
					"claimed_by": t.ClaimedBy,
					"tags":       t.Tags,
					"run":        t.Run,
					"verify_cmd": t.VerifyCmd,
					"last_run":   t.LastRunID,
				}
				if t.LastRunID != "" {
					if r, rerr := s.LoadRun(t.LastRunID); rerr == nil {
						out["run_status"] = r.Status
					}
					_, out["evidence"] = store.FindSidecar(t.LastRunID)
					if content, ok := store.ReadSidecar(t.LastRunID); ok {
						if code, has := store.SidecarExitCode(content); has {
							out["exit_code"] = code
						}
						out["log_tail"] = store.Tail(content, 15)
					}
				}
				if cps, cerr := s.ListCheckpoints(t.ID); cerr == nil && len(cps) > 0 {
					jc := make([]map[string]any, 0, len(cps))
					for _, c := range cps {
						jc = append(jc, map[string]any{
							"seq":        c.Seq,
							"summary":    c.Summary,
							"verify_cmd": c.VerifyCmd,
							"exit_code":  c.ExitCode,
							"at":         c.At,
						})
					}
					out["checkpoints"] = jc
				}
				printJSON(out)
				return nil
			}
			printLine(fmt.Sprintf("Task: %s", t.ID))
			printLine(fmt.Sprintf("Title: %s", t.Title))
			printLine(fmt.Sprintf("Status: %s", t.Status))
			printLine(fmt.Sprintf("Kind: %s", t.Kind))
			printLine(fmt.Sprintf("Tier Floor: %s", t.TierFloor))
			if t.ClaimedBy != "" {
				printLine(fmt.Sprintf("Claimed By: %s", t.ClaimedBy))
			}
			if len(t.Tags) > 0 {
				printLine(fmt.Sprintf("Tags: %s", strings.Join(t.Tags, ", ")))
			}
			printLine(fmt.Sprintf("Run: %s", orDefault(t.Run, "(none)")))
			printLine(fmt.Sprintf("Verify Cmd: %s", orDefault(t.VerifyCmd, "(none)")))
			printLine(fmt.Sprintf("Last Run: %s", orDefault(t.LastRunID, "(none)")))
			if cps, cerr := s.ListCheckpoints(t.ID); cerr == nil && len(cps) > 0 {
				printLine("")
				printLine(fmt.Sprintf("--- Checkpoints (%d) ---", len(cps)))
				for _, c := range cps {
					if c.VerifyCmd != "" {
						printLine(fmt.Sprintf("[%d] %s  (verify: %s -> exit %d, %s)", c.Seq, c.Summary, c.VerifyCmd, c.ExitCode, c.At))
					} else {
						printLine(fmt.Sprintf("[%d] %s  (%s)", c.Seq, c.Summary, c.At))
					}
				}
			}
			if t.LastRunID == "" {
				return nil
			}
			if r, rerr := s.LoadRun(t.LastRunID); rerr == nil {
				printLine("")
				printLine(fmt.Sprintf("--- Run: %s ---", r.ID))
				printLine(fmt.Sprintf("Status: %s", r.Status))
				if _, ok := store.FindSidecar(t.LastRunID); ok {
					printLine("Evidence: backed (sidecar log present)")
				} else {
					printLine("Evidence: pre-evidence (no sidecar log; trusted historical completion, not re-verifiable)")
				}
				if content, ok := store.ReadSidecar(t.LastRunID); ok {
					if code, has := store.SidecarExitCode(content); has {
						printLine(fmt.Sprintf("Exit Code: %d", code))
					}
					printLine("--- Last 15 lines of execution log ---")
					for _, l := range store.Tail(content, 15) {
						printLine(l)
					}
				} else {
					printLine("(no sidecar evidence found for this run)")
				}
			}
			return nil
		},
	}
	return c
}

// isTrivialVerify reports whether a gate command is an exact phantom that
// proves nothing: `true`, `false`, or `:` (bash noop). We deliberately do NOT
// try to shell-lint (e.g. reject `echo` chains) — that is brittle and produces
// false positives like `echo building && go test`. An empty command is NOT
// trivial here: manual tasks may legitimately have no gate, and that path
// stays a warning, not a hard reject.
func isTrivialVerify(cmd string) bool {
	switch strings.TrimSpace(cmd) {
	case "true", "false", ":":
		return true
	}
	return false
}

// runVerify runs an external command and returns its exit code. Used by
// `task checkpoint --verify`: the result is recorded as a progress note, not a
// gate (a non-zero exit warns but does not block the checkpoint).
func runVerify(command string) (int, error) {
	cmd := exec.Command("sh", "-c", command)
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// taskCheckpointCmd records a mid-task offload — a partial, agent-authored
// capture that does NOT close the task. It is the sanctioned compaction point for
// a task that is long *within itself*: "here's what I've learned, let me shed
// the raw exploration." The optional --verify command is executed and its exit
// code stored, but it never gates (the checkpoint is a note, not a gate).
func taskCheckpointCmd() *cobra.Command {
	var verify string
	c := &cobra.Command{
		Use:   "checkpoint <id> <summary>",
		Short: "record a mid-task offload (partial capture) without closing the task",
		Example: `  ward task checkpoint task-1a2b "OAuth2 PKCE flow confirmed; redirect uses a state param"
  ward task checkpoint task-1a2b "wired refresh-token path" --verify "go test ./pkg/login/..."`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return failErr(fmt.Errorf("usage: ward task checkpoint <id> <summary>"))
			}
			id := args[0]
			summary := strings.Join(args[1:], " ")
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			t, err := s.GetTask(id)
			if err != nil {
				return failErr(err)
			}
			if t.Status != "claimed" {
				return failErr(fmt.Errorf("task %s is %s, not claimed — checkpoints are for in-progress work (ward task next first)", id, t.Status))
			}
			exitCode := 0
			if verify != "" {
				if isTrivialVerify(verify) {
					return failErr(fmt.Errorf("checkpoint --verify %q is a phantom (no-op); it must exercise real work", verify))
				}
				code, rerr := runVerify(verify)
				if rerr != nil {
					return failErr(rerr)
				}
				exitCode = code
				if exitCode != 0 {
					fmt.Fprintln(os.Stderr, "warning: checkpoint --verify exited", exitCode, "(recorded; a checkpoint is a progress note, not a gate)")
				}
			}
			cp, err := s.AddCheckpoint(id, summary, verify, exitCode)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]any{
					"id":             id,
					"checkpoint_seq": cp.Seq,
					"summary":        summary,
					"verify_cmd":     verify,
					"exit_code":      exitCode,
				})
			} else {
				printLine(fmt.Sprintf("checkpoint %d recorded for %s (task stays claimed)", cp.Seq, id))
				printLine("  " + summary)
				if verify != "" {
					printLine(fmt.Sprintf("  verify: %s -> exit %d", verify, exitCode))
				}
				printLine("resume with: ward task run " + id)
			}
			return nil
		},
	}
	c.Flags().StringVar(&verify, "verify", "", "optional command proving the checkpoint's claim (recorded, not gating)")
	return c
}

// gateEvidence enforces the pre-close rule: a task that declared a gate may only
// close if its run produced a sidecar log showing exit_code == 0. Missing or
// failed evidence blocks the close and points the caller at `ward task show`.
func gateEvidence(taskID, runID string) error {
	if _, ok := store.FindSidecar(runID); !ok {
		return fmt.Errorf("cannot close task %s: verification evidence missing (no sidecar log for run %s). Run 'ward task show %s' for details", taskID, runID, taskID)
	}
	content, _ := store.ReadSidecar(runID)
	code, has := store.SidecarExitCode(content)
	if !has || code != 0 {
		return fmt.Errorf("cannot close task %s: verification failed (exit_code=%v) or evidence is missing. Run 'ward task show %s' for details", taskID, code, taskID)
	}
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
