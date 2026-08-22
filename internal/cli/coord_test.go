package cli

import (
	"os"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

func execCmd(c *cobra.Command, t *testing.T, args []string, flags map[string]string) {
	t.Helper()
	c.SetArgs(args)
	for n, v := range flags {
		if err := c.Flags().Set(n, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimLifecycle(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	execCmd(claimAddCmd(), t, []string{"auth"}, map[string]string{"by": "a1", "ttl": "30"})
	// duplicate topic warns (advisory) but proceeds
	execCmd(claimAddCmd(), t, []string{"auth"}, map[string]string{"by": "a2"})

	s, _ := store.Open()
	defer s.DB.Close()
	if got := activeClaims(s, "auth", ""); len(got) != 2 {
		t.Fatalf("expected 2 active claims, got %v", got)
	}

	// strict rejects overlap
	addS := claimAddCmd()
	if err := addS.Flags().Set("strict", "true"); err != nil {
		t.Fatal(err)
	}
	if err := addS.Flags().Set("by", "a3"); err != nil {
		t.Fatal(err)
	}
	addS.SetArgs([]string{"auth"})
	if err := addS.Execute(); err == nil {
		t.Fatal("strict claim add must error on overlap")
	}

	execCmd(claimReleaseCmd(), t, []string{"auth"}, map[string]string{})
	if got := activeClaims(s, "auth", ""); len(got) != 0 {
		t.Fatalf("release must clear claims, got %v", got)
	}
}

func TestContextCompactBlock(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	execCmd(memoryPutCmd(), t, nil, map[string]string{
		"summary": "use OAuth2", "kind": "solution",
		"verify-cmd": "grep OIDC README.md", "verify-kind": "grep", "local": "true",
	})

	// context must surface the put artifact without erroring
	execCmd(memoryContextCmd(), t, []string{"OAuth2"}, map[string]string{})
	s, _ := store.Open()
	defer s.DB.Close()
	res, _ := s.SearchArtifacts("OAuth2", "", "", 10)
	if len(res) == 0 {
		t.Fatal("context query should find the stored artifact")
	}

	// stale must list the unverified (unknown) artifact without erroring
	execCmd(memoryStaleCmd(), t, nil, map[string]string{})
}
