package cli

import (
	"fmt"
	"strings"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/routing"
	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

func routeCmd() *cobra.Command {
	var kind, verify string
	var memoryHit, contention bool
	var escalation int
	c := &cobra.Command{
		Use:   "route <node>",
		Short: "apply the pure router to one node (no LLM call)",
		RunE: func(cmd *cobra.Command, args []string) error {
			node := "default"
			if len(args) > 0 {
				node = args[0]
			}
			if kind == "" {
				kind = "default"
			}
			if verify == "" {
				verify = "unknown"
			}
			dec := routing.Route(routing.Inputs{
				NodeKind: kind, MemoryHit: memoryHit, Verify: verify,
				Contention: contention, Escalation: escalation,
			})
			if jsonOut {
				printJSON(dec)
			} else {
				if dec.Reject {
					printLine("REJECT: " + dec.Reason)
				} else {
					printLine(fmt.Sprintf("%s -> tier=%s model=%s ceremony=%s | %s", node, dec.Tier, dec.Model, dec.Ceremony, dec.Reason))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "channel|approval|test|default")
	c.Flags().BoolVar(&memoryHit, "memory-hit", false, "a verified prior solution exists")
	c.Flags().StringVar(&verify, "verify-status", "", "verified|stale|error|unknown")
	c.Flags().BoolVar(&contention, "contention", false, "real DAG contention detected")
	c.Flags().IntVar(&escalation, "escalation", 0, "escalation count (0..2)")
	return c
}

func routerCmd() *cobra.Command {
	var wfPath string
	var seed, seedStale, autoApprove bool
	c := &cobra.Command{
		Use:   "router",
		Short: "run the OIDC slice end-to-end and report routing measurement",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			if wfPath == "" {
				wfPath = "workflows/oidc-login.yaml"
			}
			wf, err := orchestration.LoadWorkflow(wfPath)
			if err != nil {
				return failErr(err)
			}
			if seed {
				seedArtifact(s, "implementation", "test", "verified",
					"OIDC login via OAuth2 authorization code grant implemented", "solution")
			}
			if seedStale {
				seedArtifact(s, "specification", "test", "stale",
					"OIDC spec revision with PKCE required", "spec")
			}
			eng := &orchestration.Engine{Store: s, RepoRoot: "", AutoApprove: autoApprove}
			runID, err := eng.StartWorkflow(wf)
			if err != nil {
				return failErr(err)
			}
			decs, err := s.RoutingDecisionsForRun(runID)
			if err != nil {
				return failErr(err)
			}
			summary := measure(decs)
			if jsonOut {
				printJSON(map[string]any{
					"run_id":        runID,
					"workflow":      wf.Name,
					"decisions":     decs,
					"measurement":   summary,
				})
			} else {
				printLine("run " + runID + " (" + wf.Name + ")")
				for _, d := range decs {
					printLine(fmt.Sprintf("  %-18s tier=%-7s model=%-15s ceremony=%-5s hit=%v verify=%s",
						d.Node, d.Tier, d.Model, d.Ceremony, d.MemoryHit, d.VerifyStatus))
					printLine("      " + d.Reason)
				}
				printLine("")
				printLine(fmt.Sprintf("cheap+verified success : %d", summary.CheapVerified))
				printLine(fmt.Sprintf("escalated              : %d", summary.Escalated))
				printLine(fmt.Sprintf("stale/unknown caught   : %d", summary.StaleCaught))
				printLine(fmt.Sprintf("rejected (budget)      : %d", summary.Rejected))
			}
			return nil
		},
	}
	c.Flags().StringVar(&wfPath, "workflow", "", "workflow YAML path")
	c.Flags().BoolVar(&seed, "seed", false, "seed a verified accepted artifact (cheap+verified path)")
	c.Flags().BoolVar(&seedStale, "seed-stale", false, "seed a stale accepted artifact (capture path)")
	c.Flags().BoolVar(&autoApprove, "auto-approve", false, "auto-approve approval nodes")
	return c
}

func seedArtifact(s *store.Store, nodeID, kind, verifyStatus, summary, tagKind string) {
	a := store.Artifact{
		Kind: tagKind, Summary: summary, Content: "seeded for " + nodeID,
		Tags:     []string{nodeID, kind},
		Status:   "accepted", CreatedBy: "seed", Local: true,
		VerifyKind: "grep", VerifyCmd: "OIDC::README.md", Ceremony: "light",
	}
	id, err := s.UpsertArtifact(a)
	if err != nil {
		return
	}
	if verifyStatus == "verified" {
		_ = s.SetVerify(id, "verified")
	} else {
		_ = s.SetVerify(id, verifyStatus)
	}
}

type measurement struct {
	CheapVerified int `json:"cheap_verified"`
	Escalated     int `json:"escalated"`
	StaleCaught   int `json:"stale_caught"`
	Rejected      int `json:"rejected"`
}

func measure(decs []store.RoutingDecision) measurement {
	var m measurement
	for _, d := range decs {
		if d.Tier == "cheap" && d.MemoryHit && d.VerifyStatus == "verified" {
			m.CheapVerified++
		}
		if d.EscalatedFrom != "" || strings.Contains(d.Reason, "escalated") {
			m.Escalated++
		}
		if d.MemoryHit && d.VerifyStatus != "verified" && d.VerifyStatus != "" {
			m.StaleCaught++
		}
		if d.Tier == "" && d.Reason != "" {
			m.Rejected++
		}
	}
	return m
}
