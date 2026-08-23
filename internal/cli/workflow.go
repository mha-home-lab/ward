package cli

import (
	"fmt"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/spf13/cobra"
)

func workflowCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "workflow", Short: "inspect workflows (show/validate)"}
	cmd.AddCommand(workflowShowCmd(), workflowValidateCmd())
	return cmd
}

func workflowShowCmd() *cobra.Command {
	var path string
	c := &cobra.Command{
		Use:   "show",
		Short: "show a workflow's nodes and edges",
		Example: `  ward workflow show --workflow workflows/default.yaml
  ward workflow show --workflow workflows/parallel-demo.yaml --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			wf, err := loadWF(path)
			if err != nil {
				return failErr(err)
			}
			order, _ := wf.TopoOrder()
			if jsonOut {
				printJSON(map[string]any{"name": wf.Name, "order": order, "nodes": wf.Nodes, "edges": wf.Edges})
			} else {
				printLine("workflow " + wf.Name)
				printLine("order: " + fmt.Sprint(order))
				for _, n := range wf.Nodes {
					printLine(fmt.Sprintf("  %-18s %-10s produces=%v", n.ID, n.Kind, n.Produces))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&path, "workflow", "", "workflow YAML path")
	return c
}

func workflowValidateCmd() *cobra.Command {
	var path string
	c := &cobra.Command{
		Use:   "validate",
		Short: "validate a workflow DAG",
		Example: `  ward workflow validate --workflow workflows/default.yaml
  ward workflow validate --workflow workflows/parallel-demo.yaml --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := loadWF(path)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]string{"status": "valid"})
			} else {
				printLine("valid")
			}
			return nil
		},
	}
	c.Flags().StringVar(&path, "workflow", "", "workflow YAML path")
	return c
}

var _ = orchestration.LoadWorkflow
