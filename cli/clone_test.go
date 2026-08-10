package cli

import (
	"strings"
	"testing"
)

func TestTraversalFlagRemainsAcceptedButDeprecated(t *testing.T) {
	cmd := newCloneCmd()
	flag := cmd.Flags().Lookup("traversal")
	if flag == nil {
		t.Fatal("--traversal was removed; existing scripts must keep parsing")
	}
	if !strings.Contains(flag.Deprecated, "always breadth-first") {
		t.Errorf("deprecation message = %q, want breadth-first explanation", flag.Deprecated)
	}
	if err := cmd.Flags().Set("traversal", "dfs"); err != nil {
		t.Fatalf("legacy --traversal dfs no longer parses: %v", err)
	}
}
