package agentsandbox

import (
	"testing"

	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	_ "github.com/yuluo-yx/agentscope-go/e2e/testcases"
)

func TestProfileIncludesRegisteredDefaultScenarios(t *testing.T) {
	profile := &Profile{}
	want := []string{
		"workspace-agent-sandbox-agent-loop",
		"workspace-agent-sandbox-builtin-tools-loop",
		"workspace-agent-sandbox-resource-lifecycle",
		"workspace-agent-sandbox-tool-error-boundaries",
	}

	got := profile.GetTestCases()
	for _, name := range want {
		found := false
		for _, current := range got {
			if current == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("agent-sandbox profile should include %q, got %#v", name, got)
		}
		if _, ok := pkgtestcases.Get(name); !ok {
			t.Fatalf("agent-sandbox profile testcase %q should be registered", name)
		}
	}
}
