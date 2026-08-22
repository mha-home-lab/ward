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

	// Two different topics -> two active claims (exclusive acquire per topic).
	execCmd(claimAddCmd(), t, []string{"auth"}, map[string]string{"by": "a1", "ttl": "30"})
	execCmd(claimAddCmd(), t, []string{"oauth"}, map[string]string{"by": "a2", "ttl": "30"})
	s, _ := store.Open()
	defer s.DB.Close()
	if got := activeClaims(s, "", ""); len(got) != 2 {
		t.Fatalf("expected 2 active claims, got %v", got)
	}

	// Same topic by another agent is an EXCLUSIVE conflict: hard error, no 2nd claim.
	addDup := claimAddCmd()
	if err := addDup.Flags().Set("by", "a3"); err != nil {
		t.Fatal(err)
	}
	addDup.SetArgs([]string{"auth"})
	if err := addDup.Execute(); err == nil {
		t.Fatal("duplicate topic claim add must error (exclusive)")
	}
	if got := activeClaims(s, "auth", ""); len(got) != 1 {
		t.Fatalf("duplicate topic must not create a 2nd active claim, got %v", got)
	}

	// release frees exactly the topic's claim; unrelated survives.
	execCmd(claimReleaseCmd(), t, []string{"auth"}, map[string]string{})
	if got := activeClaims(s, "auth", ""); len(got) != 0 {
		t.Fatalf("release must clear claims, got %v", got)
	}
	if got := activeClaims(s, "", ""); len(got) != 1 {
		t.Fatalf("unrelated claim should survive, got %v", got)
	}
}

// Acceptance: an expired claim blocks re-claim until tick frees it.
func TestClaimExpiredThenTickFrees(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	// first claim ok
	execCmd(claimAddCmd(), t, []string{"foo"}, map[string]string{"by": "a1"})
	// second claim on same topic must error (exclusive)
	dup := claimAddCmd()
	dup.SetArgs([]string{"foo"})
	if err := dup.Execute(); err == nil {
		t.Fatal("first claim must make second error")
	}

	// backdate the claim's expiry directly, then tick must free it
	s, _ := store.Open()
	defer s.DB.Close()
	if _, err := s.DB.Exec(`UPDATE artifacts SET expires_at='2000-01-01T00:00:00Z' WHERE claim_topic='foo'`); err != nil {
		t.Fatal(err)
	}
	execCmd(tickCmd(), t, nil, map[string]string{})

	// now foo can be claimed again
	execCmd(claimAddCmd(), t, []string{"foo"}, map[string]string{"by": "a2"})
	if got := activeClaims(s, "foo", ""); len(got) != 1 {
		t.Fatalf("after tick, foo should be re-claimable, got %v", got)
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
