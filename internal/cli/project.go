package cli

import (
	"fmt"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// projectCmd manages the registry that maps a logical project name to the
// directory holding its `.ward` store. It exists so requests can be routed to the
// right store from any working directory (see store.OpenForName).
func projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "manage the project-store registry (name -> .ward directory)",
	}
	cmd.AddCommand(projectRegisterCmd(), projectListCmd())
	return cmd
}

func projectRegisterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "register <name> <path-to-.ward-dir>",
		Short: "record a project name -> .ward directory mapping",
		Example: `  ward project register ward /path/to/ward-repo/.ward
  ward project register ward   # reuse the current repo's .ward if no path given`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("usage: ward project register <name> [path-to-.ward-dir]"))
			}
			name := args[0]
			home := store.Home()
			if len(args) > 1 {
				home = args[1]
			}
			if err := store.RegisterProject(name, home); err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]string{"registered": name, "home": home})
			} else {
				printLine(fmt.Sprintf("project %q -> %s (resolved via --project %s or WARD_PROJECT_%s_HOME)", name, home, name, upper(name)))
			}
			return nil
		},
	}
}

func projectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list registered project stores",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := store.ListProjects()
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(reg)
				return nil
			}
			if len(reg) == 0 {
				printLine("no projects registered; add one with 'ward project register <name> <path>'")
				return nil
			}
			for name, home := range reg {
				printLine(fmt.Sprintf("%s -> %s", name, home))
			}
			return nil
		},
	}
}

func upper(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			out = append(out, r-'a'+'A')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}
