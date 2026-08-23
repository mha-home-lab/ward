package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "run", Short: "workflow run lifecycle (start/status/approve/resume)"}
	cmd.AddCommand(runStartCmd(), runStatusCmd(), runApproveCmd(), runResumeCmd())
	return cmd
}

// defaultWorkflowPaths is the resolution order for an unspecified --workflow:
// the runnable scaffold a fresh project gets from `ward init --scaffold` first,
// the demo workflow last. The final entry only produces a useful error message.
var defaultWorkflowPaths = []string{"workflows/default.yaml", "workflows/oidc-login.yaml"}

// loadWF resolves an unspecified path against defaultWorkflowPaths instead of
// hardcoding one demo file, so a project that scaffolded default.yaml runs it
// without flags.
func loadWF(path string) (*orchestration.Workflow, error) {
	if path == "" {
		for _, p := range defaultWorkflowPaths {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
		if path == "" {
			path = defaultWorkflowPaths[0]
		}
	}
	return orchestration.LoadWorkflow(path)
}

// resolveRunWF reloads the workflow a run was started from. The run persists
// its originating file path, so a second session can resume/approve without
// re-supplying --workflow; an explicit flag still overrides.
func resolveRunWF(s *store.Store, flagPath, runID string) (*orchestration.Workflow, error) {
	if flagPath != "" {
		return loadWF(flagPath)
	}
	r, err := s.LoadRun(runID)
	if err != nil {
		return nil, err
	}
	if r.WorkflowPath != "" {
		return orchestration.LoadWorkflow(r.WorkflowPath)
	}
	return loadWF("")
}

func runStartCmd() *cobra.Command {
	var wfPath string
	var autoApprove bool
	c := &cobra.Command{
		Use:   "start",
		Short: "start a workflow run",
		Example: `  ward run start --auto-approve
  ward run start --workflow workflows/default.yaml
  ward run start --workflow workflows/agent-demo.yaml --auto-approve --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			wf, err := loadWF(wfPath)
			if err != nil {
				return failErr(err)
			}
			eng := &orchestration.Engine{Store: s, AutoApprove: autoApprove}
			runID, err := eng.StartWorkflow(wf)
			if err != nil {
				return failErr(err)
			}
			autoCapture(s, wf, runID)
			r, _ := s.LoadRun(runID)
			if jsonOut {
				printJSON(map[string]string{"run_id": runID, "status": r.Status, "waiting": r.WaitingApproval})
			} else {
				printLine(fmt.Sprintf("run %s started: %s (waiting=%s)", runID, r.Status, r.WaitingApproval))
			}
			return nil
		},
	}
	c.Flags().StringVar(&wfPath, "workflow", "", "workflow YAML path")
	c.Flags().BoolVar(&autoApprove, "auto-approve", false, "auto-approve approval nodes")
	return c
}

func runStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <runID>",
		Short: "show run status and node states",
		Example: `  ward run status run-3f2a
  ward run status run-3f2a --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			r, err := s.LoadRun(args[0])
			if err != nil {
				return failErr(err)
			}
			nodes, _ := s.LoadRunNodes(args[0])
			if nodes == nil {
				nodes = []store.RunNode{}
			}
			if jsonOut {
				printJSON(map[string]any{"run": r, "nodes": nodes})
			} else {
				printLine(fmt.Sprintf("run %s %s (waiting=%s)", r.ID, r.Status, r.WaitingApproval))
				for _, n := range nodes {
					printLine(fmt.Sprintf("  %-18s %-18s touched=%d %s", n.Node, n.Status, len(n.Touched), n.DeclaredObs))
				}
			}
			return nil
		},
	}
}

func runApproveCmd() *cobra.Command {
	var wfPath string
	c := &cobra.Command{
		Use:   "approve <runID> <node>",
		Short: "approve an awaiting_approval node and resume",
		Example: `  ward run approve run-3f2a review-gate
  ward run approve run-3f2a review-gate --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return failErr(fmt.Errorf("need <runID> <node>"))
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			wf, err := resolveRunWF(s, wfPath, args[0])
			if err != nil {
				return failErr(err)
			}
			eng := &orchestration.Engine{Store: s}
			if err := eng.Approve(args[0], args[1], wf); err != nil {
				return failErr(err)
			}
			autoCapture(s, wf, args[0])
			r, _ := s.LoadRun(args[0])
			if jsonOut {
				printJSON(map[string]string{"status": r.Status})
			} else {
				printLine("approved; run now " + r.Status)
			}
			return nil
		},
	}
	c.Flags().StringVar(&wfPath, "workflow", "", "workflow YAML path")
	return c
}

func runResumeCmd() *cobra.Command {
	var wfPath string
	var autoApprove bool
	c := &cobra.Command{
		Use:   "resume <runID>",
		Short: "resume a paused run",
		Example: `  ward run resume run-3f2a --auto-approve
  ward run resume run-3f2a --workflow workflows/task-12.yaml --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(errNeedID)
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			wf, err := resolveRunWF(s, wfPath, args[0])
			if err != nil {
				return failErr(err)
			}
			eng := &orchestration.Engine{Store: s, AutoApprove: autoApprove}
			if err := eng.Run(args[0], wf); err != nil {
				return failErr(err)
			}
			autoCapture(s, wf, args[0])
			r, _ := s.LoadRun(args[0])
			if jsonOut {
				printJSON(map[string]string{"status": r.Status})
			} else {
				printLine("run now " + r.Status)
			}
			return nil
		},
	}
	c.Flags().StringVar(&wfPath, "workflow", "", "workflow YAML path")
	c.Flags().BoolVar(&autoApprove, "auto-approve", false, "auto-approve approval nodes")
	return c
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "check store + environment health",
		Example: `  ward doctor
  ward doctor --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			health := map[string]any{}
			if err != nil {
				health["store"] = "error: " + err.Error()
			} else {
				defer s.DB.Close()
				health["store"] = "ok"
				health["home"] = s.Home
				var uv int
				_ = s.DB.QueryRow("PRAGMA user_version").Scan(&uv)
				health["user_version"] = uv
				// Legacy claims (pre-v0.4) have claim_topic NULL, so the
				// atomicity index doesn't enforce them. Surface the count so
				// the one-time transition gap is visible, not silent.
				if n, err := s.LegacyClaimCount(); err == nil {
					health["legacy_claims"] = n
				}
			}
			health["git"] = gitAvailable()
			if jsonOut {
				printJSON(health)
			} else {
				printLine(fmt.Sprintf("store: %v", health["store"]))
				printLine(fmt.Sprintf("user_version: %v", health["user_version"]))
				printLine(fmt.Sprintf("legacy_claims (pre-v0.4, not atomically enforced): %v", health["legacy_claims"]))
				printLine(fmt.Sprintf("git: %v", health["git"]))
			}
			return nil
		},
	}
}

func gitAvailable() string {
	if _, err := lookupGit(); err != nil {
		return "missing: " + err.Error()
	}
	return "ok"
}

func lookupGit() (string, error) {
	return exec.LookPath("git")
}
