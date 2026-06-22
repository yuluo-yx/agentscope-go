package testcases

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoRootLocatesRepositoryAfterPkgMigration(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read root go.mod: %v", err)
	}
	if !strings.Contains(string(data), "module github.com/yuluo-yx/agentscope-go") {
		t.Fatalf("unexpected module in root go.mod: %s", strings.TrimSpace(string(data)))
	}
	if _, err := os.Stat(filepath.Join(root, "pkg", "agent", "agent.go")); err != nil {
		t.Fatalf("stat migrated agent package sentinel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "facade.go")); err != nil {
		t.Fatalf("stat root facade sentinel: %v", err)
	}
}
