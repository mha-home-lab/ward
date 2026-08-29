package cli

import (
	"fmt"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/verification"
	"github.com/spf13/cobra"
)

// docCmd makes documentation/spec/architecture claims first-class verifiable
// artifacts. A "doc claim" is just an artifact of Kind "doc" whose verify_cmd
// is `pattern::path` (grep kind): "README must mention the auth header" becomes
// an artifact that `ward verify` / `ward brief` re-check live, so drifting
// prose is caught exactly like any other verification.
func docCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "doc",
		Short: "register and verify documentation/spec claims as grep artifacts",
	}
	c.AddCommand(docAssertCmd(), docVerifyCmd())
	return c
}

func docAssertCmd() *cobra.Command {
	var name, by, tags, repo string
	c := &cobra.Command{
		Use:   "assert <path> <pattern>",
		Short: "register a doc claim: <path> must contain <pattern> (grep), then verify it live",
		Example: `  ward doc assert README.md "auth header"
  ward doc assert README.md "auth header" --name "secure-bank attack surface" --by mohamed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return failErr(fmt.Errorf("usage: ward doc assert <path> <pattern>"))
			}
			path, pattern := args[0], strings.Join(args[1:], " ")
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			summary := name
			if summary == "" {
				summary = fmt.Sprintf("doc claim: %s must contain %q", path, pattern)
			}
			tagList := []string{"doc"}
			if tags != "" {
				tagList = append(tagList, splitCSV(tags)...)
			}
			a := store.Artifact{
				Kind:       "doc",
				Summary:    summary,
				Content:    path,
				Tags:       tagList,
				Status:     "accepted",
				CreatedBy:  by,
				VerifyKind: "grep",
				VerifyCmd:  pattern + "::" + path,
				Local:      true,
			}
			id, err := s.UpsertArtifact(a)
			if err != nil {
				return failErr(err)
			}
			res := verification.Run(a, repo)
			if err := s.SetVerify(id, res.Status); err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(map[string]any{
					"id":      id,
					"path":    path,
					"pattern": pattern,
					"status":  res.Status,
					"detail":  res.Detail,
				})
			} else {
				printLine(fmt.Sprintf("%s: %s (%s)", path, res.Status, res.Detail))
			}
			if res.Status != "verified" {
				return failErr(fmt.Errorf("doc claim FAILED: %s — %s", path, res.Detail))
			}
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "human title for the claim (default derived from path+pattern)")
	c.Flags().StringVar(&by, "by", "", "who is asserting the claim")
	c.Flags().StringVar(&tags, "tags", "", "extra comma-separated tags")
	c.Flags().StringVar(&repo, "repo", "", "repo root the grep runs in (default: current dir)")
	return c
}

func docVerifyCmd() *cobra.Command {
	var repo string
	c := &cobra.Command{
		Use:   "verify",
		Short: "re-run every doc claim and report pass/fail (thin wrapper over the verify sweep)",
		Example: `  ward doc verify
  ward doc verify --repo ../other-repo --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			docs, err := s.ListArtifacts("", "doc", "", 1000)
			if err != nil {
				return failErr(err)
			}
			out := []map[string]string{}
			for _, a := range docs {
				res := verification.Run(a, repo)
				_ = s.SetVerify(a.ID, res.Status)
				out = append(out, map[string]string{"id": a.ID, "path": a.Content, "status": res.Status, "detail": res.Detail})
			}
			if jsonOut {
				printJSON(out)
			} else {
				for _, o := range out {
					printLine(fmt.Sprintf("%s (%s): %s — %s", o["path"], o["id"], o["status"], o["detail"]))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "repo root the grep runs in (default: current dir)")
	return c
}
