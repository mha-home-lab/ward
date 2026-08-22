package orchestration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Workflow is the v1 DAG definition (subset of ciao's node graph).
type Workflow struct {
	Name  string `yaml:"name"`
	Path  string // file the workflow was loaded from (empty if in-memory)
	Nodes []Node `yaml:"nodes"`
	Edges []Edge `yaml:"edges"`
}

type Node struct {
	ID       string   `yaml:"id"`
	Kind     string   `yaml:"kind"` // channel | approval | test
	Channels []string `yaml:"channels"`
	Produces []string `yaml:"produces"` // agent-declared touched set (stand-in for D0.1)
	Run      string   `yaml:"run"`      // optional shell command the node executes (real adapter)
	Prompt   string   `yaml:"prompt"`   // optional model task; driven via the opencode adapter at the routed tier
	// Tier is the declared minimum tier for this node — a routing FLOOR. When
	// set, the router never selects below it (see routing.go). Empty = pure
	// inference (v0.3.0 behavior).
	Tier string `yaml:"tier"`
}

type Edge struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// nodeMap indexes nodes by id.
func (w *Workflow) nodeMap() map[string]Node {
	m := map[string]Node{}
	for _, n := range w.Nodes {
		m[n.ID] = n
	}
	return m
}

// Validate checks the DAG: single root, acyclic, approvals have channels.
func (w *Workflow) Validate() error {
	if w.Name == "" {
		return fmt.Errorf("workflow missing name")
	}
	if len(w.Nodes) == 0 {
		return fmt.Errorf("workflow has no nodes")
	}
	indeg := map[string]int{}
	m := w.nodeMap()
	for _, n := range w.Nodes {
		indeg[n.ID] = 0
	}
	for _, e := range w.Edges {
		if _, ok := m[e.From]; !ok {
			return fmt.Errorf("edge from unknown node %q", e.From)
		}
		if _, ok := m[e.To]; !ok {
			return fmt.Errorf("edge to unknown node %q", e.To)
		}
		indeg[e.To]++
	}
	roots := []string{}
	for _, n := range w.Nodes {
		if indeg[n.ID] == 0 {
			roots = append(roots, n.ID)
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("expected exactly 1 root, found %d: %v", len(roots), roots)
	}
	if err := w.acyclic(m, indeg); err != nil {
		return err
	}
	for _, n := range w.Nodes {
		if n.Kind == "approval" && len(n.Channels) == 0 {
			return fmt.Errorf("approval node %q has no channels", n.ID)
		}
	}
	return nil
}

func (w *Workflow) acyclic(m map[string]Node, indeg map[string]int) error {
	// Kahn's algorithm; if not all nodes processed, there is a cycle.
	rem := map[string]int{}
	for k, v := range indeg {
		rem[k] = v
	}
	queue := []string{}
	for k, v := range rem {
		if v == 0 {
			queue = append(queue, k)
		}
	}
	seen := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		seen++
		for _, e := range w.Edges {
			if e.From == n {
				rem[e.To]--
				if rem[e.To] == 0 {
					queue = append(queue, e.To)
				}
			}
		}
	}
	if seen != len(w.Nodes) {
		return fmt.Errorf("workflow has a cycle")
	}
	return nil
}

// TopoOrder returns a deterministic topological order of node ids.
func (w *Workflow) TopoOrder() ([]string, error) {
	indeg := map[string]int{}
	for _, n := range w.Nodes {
		indeg[n.ID] = 0
	}
	adj := map[string][]string{}
	for _, e := range w.Edges {
		indeg[e.To]++
		adj[e.From] = append(adj[e.From], e.To)
	}
	for k := range adj {
		sort.Strings(adj[k])
	}
	queue := []string{}
	for _, n := range w.Nodes {
		if indeg[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	sort.Strings(queue)
	out := []string{}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		out = append(out, n)
		for _, to := range adj[n] {
			indeg[to]--
			if indeg[to] == 0 {
				queue = append(queue, to)
			}
		}
		sort.Strings(queue)
	}
	if len(out) != len(w.Nodes) {
		return nil, fmt.Errorf("cycle in workflow")
	}
	return out, nil
}

// LoadWorkflow reads and validates a workflow YAML file.
func LoadWorkflow(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w Workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	w.Path = path
	return &w, nil
}

// Save writes the workflow as YAML to path (mkdir -p), then re-validates what
// was written by loading it back — a saved workflow must be runnable.
func (w *Workflow) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(w)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	_, err = LoadWorkflow(path)
	return err
}

// TaskWorkflow generates a runnable single-node DAG from a dispatch-pool task:
// start -> work -> done. The work node carries the task's run command (if any)
// and its declared tier floor, so executing it routes exactly like any other
// node while auto-capture records the result.
func TaskWorkflow(taskID, title, kind, run, verifyCmd string) *Workflow {
	if kind == "" || kind == "channel" {
		kind = "default"
	}
	work := Node{ID: "work", Kind: kind}
	if run != "" {
		work.Run = run
	} else if verifyCmd != "" && kind == "test" {
		work.Run = verifyCmd
	}
	w := &Workflow{
		Name:  "task-" + strings.TrimPrefix(taskID, "task-"),
		Nodes: []Node{{ID: "start", Kind: "channel"}, work, {ID: "done", Kind: "channel"}},
		Edges: []Edge{{From: "start", To: "work"}, {From: "work", To: "done"}},
	}
	return w
}

// Reachable returns the set of nodes reachable from `from` via directed edges
// (its descendants, including transitively).
func (w *Workflow) Reachable(from string) map[string]bool {
	adj := map[string][]string{}
	for _, e := range w.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	m := map[string]bool{}
	q := []string{from}
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		for _, to := range adj[n] {
			if !m[to] {
				m[to] = true
				q = append(q, to)
			}
		}
	}
	return m
}
