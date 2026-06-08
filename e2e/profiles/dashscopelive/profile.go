package dashscopelive

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/framework"
)

type Profile struct{}

func init() {
	framework.RegisterProfile("dashscope-live", func() framework.Profile { return &Profile{} })
}

func (*Profile) Name() string { return "dashscope-live" }

func (*Profile) Description() string {
	return "DashScope live model and Agent E2E suite"
}

func (*Profile) Setup(context.Context, *framework.SetupOptions) error {
	if dashScopeAPIKey() == "" {
		return fmt.Errorf("set DASHSCOPE_API_KEY or AI_DASHSCOPE_API_KEY to run dashscope-live profile")
	}
	return nil
}

func (*Profile) Teardown(context.Context, *framework.TeardownOptions) error { return nil }

func (*Profile) GetTestCases() []string {
	return []string{
		"dashscope-chat-call",
		"dashscope-chat-stream",
		"dashscope-embedding-text",
		"dashscope-agent-tool-loop",
		"dashscope-agent-events",
	}
}

func dashScopeAPIKey() string {
	if value := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("AI_DASHSCOPE_API_KEY"))
}
