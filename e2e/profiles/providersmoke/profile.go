package providersmoke

import (
	"context"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/framework"
)

type Profile struct{}

func init() {
	framework.RegisterProfile("provider-smoke", func() framework.Profile { return &Profile{} })
}

func (*Profile) Name() string { return "provider-smoke" }

func (*Profile) Description() string {
	return "Explicit live provider smoke suite gated by AGENTSCOPE_TEST_* variables"
}

func (*Profile) Setup(context.Context, *framework.SetupOptions) error { return nil }

func (*Profile) Teardown(context.Context, *framework.TeardownOptions) error { return nil }

func (*Profile) GetTestCases() []string {
	return []string{
		"provider-openai-chat-smoke",
		"provider-openai-responses-smoke",
		"provider-dashscope-chat-smoke",
		"provider-deepseek-chat-smoke",
		"provider-gemini-chat-smoke",
		"provider-moonshot-chat-smoke",
		"provider-xai-chat-smoke",
		"provider-zhipu-chat-smoke",
	}
}
