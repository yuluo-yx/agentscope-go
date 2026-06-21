package local

import (
	"context"
	"os"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/framework"
)

type Profile struct{}

func init() {
	framework.RegisterProfile("local", func() framework.Profile { return &Profile{} })
}

func (*Profile) Name() string { return "local" }

func (*Profile) Description() string {
	return "Local deterministic E2E suite without live provider keys"
}

func (*Profile) Setup(context.Context, *framework.SetupOptions) error {
	_ = os.Unsetenv("DASHSCOPE_API_KEY")
	_ = os.Unsetenv("AI_DASHSCOPE_API_KEY")
	return nil
}

func (*Profile) Teardown(context.Context, *framework.TeardownOptions) error { return nil }

func (*Profile) GetTestCases() []string {
	return []string{
		"chatmodel-contract",
		"chatmodel-stream-error",
		"embedding-cache-contract",
		"agent-tool-loop",
		"permission-confirm-resume",
		"permission-deny-tool-result",
		"permission-updated-input",
		"external-tool-resume",
		"agent-observe-permission-context",
		"workspace-local-files",
		"builtin-tools-agent-loop",
		"mcp-inprocess-agent",
		"workspace-offload",
		"workspace-resource-lifecycle",
		"facade-package-contract",
		"middleware-react-tracing",
		"task-tools-state",
		"task-tools-lifecycle",
		"mcp-external-transports",
		"gateway-http-contract",
		"gateway-http-edge-contracts",
		"message-event-apply",
		"message-state-types-contracts",
		"context-compression",
		"loop-automation-contracts",
		"model-provider-metadata-contracts",
	}
}
