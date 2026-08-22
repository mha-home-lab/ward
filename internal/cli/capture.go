package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// captureNode writes a store-local, accepted (light-ceremony) artifact recording
// that a node's work succeeded. This is flow.md step 7: success writes a claim
// the next session can live-verify, so cheap becomes a consequence of doing the
// work, not of hand-typing YAML.
//
// Tag defaults to the node id (the only tag routing needs for a hit). verify_cmd
// is inferred: a test node -> `go test ./...`; otherwise a hash of `produces`
// when it names a concrete file. An explicit flag overrides either.
func captureNode(s *store.Store, wf *orchestration.Workflow, node orchestration.Node,
	tagOverride, verifyCmdOverride, verifyKindOverride, summaryOverride, contentOverride string) (string, error) {

	tag := tagOverride
	if tag == "" {
		tag = node.ID
	}

	kind := "context"
	if node.Kind == "test" {
		kind = "solution"
	}

	summary := summaryOverride
	if summary == "" {
		summary = fmt.Sprintf("%s result (%s)", node.ID, node.Kind)
	}

	verifyCmd, verifyKind, content := inferVerify(node, summary, contentOverride)

	if verifyCmdOverride != "" {
		verifyCmd = verifyCmdOverride
		verifyKind = verifyKindOverride
		if verifyKind == "" {
			verifyKind = "shell"
		}
	}

	a := store.Artifact{
		Kind:       kind,
		Summary:    summary,
		Content:    content,
		Tags:       []string{tag},
		Status:     "accepted",
		CreatedBy:  "ward",
		VerifyCmd:  verifyCmd,
		VerifyKind: verifyKind,
		Ceremony:   "light",
		Local:      true,
	}
	id, err := s.UpsertArtifact(a)
	if err != nil {
		return "", err
	}
	// Verification is deferred to the next session (flow.md step 7: "a claim the
	// next session can live-verify"). We do NOT run verify_cmd here — running it
	// immediately would (e.g. for a `go test` node) re-execute the very work that
	// just succeeded, and recurse inside `go test` itself.
	return id, nil
}

// inferVerify returns (verifyCmd, verifyKind, content) for a node, with no
// override applied.
//
// v0.3 inference nit: verify_cmd defaults from the node's OWN run: (shell), so a
// captured claim records what actually ran (a grep node captures "grep ...", not
// a generic go test). `go test ./...` is only the fallback when run: is empty AND
// the node is a test. A concrete `produces` falls back to a sha256 hash (glob
// produces are expanded to a concrete file; a hash of a glob is meaningless).
func inferVerify(node orchestration.Node, summary, contentOverride string) (string, string, string) {
	if node.Run != "" {
		c := contentOverride
		if c == "" {
			c = fmt.Sprintf("Auto-captured result of node %s after successful run (ran: %s).", node.ID, node.Run)
		}
		return node.Run, "shell", c
	}
	if node.Kind == "test" {
		c := contentOverride
		if c == "" {
			c = fmt.Sprintf("Auto-captured result of node %s after successful run.", node.ID)
		}
		return "go test ./...", "test", c
	}
	if len(node.Produces) > 0 {
		path := node.Produces[0]
		if strings.ContainsAny(path, "*?[") {
			if matches, _ := filepath.Glob(path); len(matches) > 0 {
				path = matches[0]
			} else {
				return "", "", contentOverride
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", "", contentOverride
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		c := contentOverride
		if c == "" {
			c = got + "\n" + fmt.Sprintf("Auto-captured result of node %s (hash of %s).", node.ID, path)
		}
		return "sha256::" + path, "hash", c
	}
	return "", "", contentOverride
}

// autoCapture records every done node in a run that carried a `run:` command.
// Called after a run/resume advance from the CLI so the engine stays untouched.
func autoCapture(s *store.Store, wf *orchestration.Workflow, runID string) {
	nodes, err := s.LoadRunNodes(runID)
	if err != nil {
		return
	}
	status := map[string]string{}
	for _, n := range nodes {
		status[n.Node] = n.Status
	}
	for _, node := range wf.Nodes {
		if node.Run == "" || status[node.ID] != "done" {
			continue
		}
		if _, err := captureNode(s, wf, node, "", "", "", "", ""); err != nil {
			printLine(fmt.Sprintf("capture skip %s: %v", node.ID, err))
		}
	}
}

func captureCmd() *cobra.Command {
	var runID, nodeID, wfPath, tag, verifyCmd, verifyKind, summary, content string
	c := &cobra.Command{
		Use:   "capture",
		Short: "write a verified claim for a completed node (result capture, flow.md step 7)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			var wf *orchestration.Workflow
			if runID != "" {
				r, err := s.LoadRun(runID)
				if err != nil {
					return failErr(err)
				}
				path := wfPath
				if path == "" {
					path = r.WorkflowPath
				}
				wf, err = orchestration.LoadWorkflow(path)
				if err != nil {
					return failErr(err)
				}
			} else if nodeID != "" {
				if wfPath == "" {
					return failErr(fmt.Errorf("need --workflow to capture a node directly"))
				}
				wf, err = orchestration.LoadWorkflow(wfPath)
				if err != nil {
					return failErr(err)
				}
			} else {
				return failErr(fmt.Errorf("need --run <id> or --node <id> --workflow <path>"))
			}

			var captured []string
			if runID != "" {
				nodes, _ := s.LoadRunNodes(runID)
				st := map[string]string{}
				for _, n := range nodes {
					st[n.Node] = n.Status
				}
				for _, node := range wf.Nodes {
					if nodeID != "" && node.ID != nodeID {
						continue
					}
					if st[node.ID] != "done" {
						continue
					}
					id, err := captureNode(s, wf, node, tag, verifyCmd, verifyKind, summary, content)
					if err != nil {
						return failErr(err)
					}
					captured = append(captured, id)
				}
			} else {
				var node orchestration.Node
				ok := false
				for _, n := range wf.Nodes {
					if n.ID == nodeID {
						node, ok = n, true
						break
					}
				}
				if !ok {
					return failErr(fmt.Errorf("node %s not in workflow", nodeID))
				}
				id, err := captureNode(s, wf, node, tag, verifyCmd, verifyKind, summary, content)
				if err != nil {
					return failErr(err)
				}
				captured = append(captured, id)
			}

			if jsonOut {
				printJSON(map[string]any{"captured": captured})
			} else {
				printLine(fmt.Sprintf("captured %d artifact(s): %s", len(captured), strings.Join(captured, " ")))
			}
			return nil
		},
	}
	c.Flags().StringVar(&runID, "run", "", "capture all done nodes from a run")
	c.Flags().StringVar(&nodeID, "node", "", "capture a single node (needs --workflow)")
	c.Flags().StringVar(&wfPath, "workflow", "", "workflow YAML path")
	c.Flags().StringVar(&tag, "tag", "", "tag for the artifact (default: node id)")
	c.Flags().StringVar(&verifyCmd, "verify-cmd", "", "override inferred verify command")
	c.Flags().StringVar(&verifyKind, "verify-kind", "", "verify kind for --verify-cmd (shell|grep|build|test|hash)")
	c.Flags().StringVar(&summary, "summary", "", "artifact summary (default derived)")
	c.Flags().StringVar(&content, "content", "", "artifact content (default derived)")
	return c
}
