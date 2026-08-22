package adapter

import (
	"reflect"
	"testing"
)

func TestModelForTier(t *testing.T) {
	cases := map[string]string{
		"cheap":  "opencode/hy3-free",
		"mid":    "opencode/mimo-v2.5-free",
		"strong": "opencode/nemotron-3-ultra-free",
		"bogus":  "opencode/hy3-free",
		"":       "opencode/hy3-free",
	}
	for tier, want := range cases {
		if got := ModelForTier(tier); got != want {
			t.Fatalf("ModelForTier(%q) = %q, want %q", tier, got, want)
		}
	}
}

func TestCommandArgs(t *testing.T) {
	got := commandArgs("/repo", "opencode/hy3-free", "write the thing")
	want := []string{"run", "-m", "opencode/hy3-free", "--dir", "/repo", "write the thing"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandArgs = %v, want %v", got, want)
	}
}
