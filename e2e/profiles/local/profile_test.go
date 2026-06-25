package local

import (
	"testing"

	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	_ "github.com/yuluo-yx/agentscope-go/e2e/testcases"
)

func TestProfileIncludesRegisteredDefaultScenarios(t *testing.T) {
	profile := &Profile{}
	cases := profile.GetTestCases()
	for _, name := range cases {
		if _, ok := pkgtestcases.Get(name); !ok {
			t.Fatalf("local profile testcase %q should be registered", name)
		}
	}

	for _, name := range []string{
		"permission-deny-tool-result",
		"permission-updated-input",
		"external-tool-resume",
		"agent-tool-result-metadata",
		"tool-response-data-chunk-contract",
		"message-tool-result-contracts",
		"provider-formatting-contracts",
		"loop-automation-contracts",
	} {
		if !profileIncludesTestCase(cases, name) {
			t.Fatalf("local profile should include %q, got %#v", name, cases)
		}
	}
}

func profileIncludesTestCase(cases []string, name string) bool {
	for _, current := range cases {
		if current == name {
			return true
		}
	}
	return false
}
