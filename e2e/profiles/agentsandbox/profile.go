package agentsandbox

import (
	"context"
	"fmt"
	"os"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/framework"
)

type Profile struct{}

func init() {
	framework.RegisterProfile("agent-sandbox", func() framework.Profile { return &Profile{} })
}

func (*Profile) Name() string { return "agent-sandbox" }

func (*Profile) Description() string {
	return "Agent Sandbox workspace E2E suite"
}

func (*Profile) Setup(context.Context, *framework.SetupOptions) error {
	if os.Getenv("AGENTSCOPE_E2E_AGENT_SANDBOX") != "1" {
		return fmt.Errorf("set AGENTSCOPE_E2E_AGENT_SANDBOX=1 to run agent-sandbox profile")
	}
	return nil
}

func (*Profile) Teardown(context.Context, *framework.TeardownOptions) error { return nil }

func (*Profile) GetTestCases() []string {
	return []string{"workspace-agent-sandbox-agent-loop"}
}
