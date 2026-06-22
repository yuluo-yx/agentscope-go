package testcases

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/framework"
	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/model/deepseek"
	"github.com/yuluo-yx/agentscope-go/pkg/model/gemini"
	"github.com/yuluo-yx/agentscope-go/pkg/model/moonshot"
	"github.com/yuluo-yx/agentscope-go/pkg/model/openai"
	"github.com/yuluo-yx/agentscope-go/pkg/model/openairesponse"
	"github.com/yuluo-yx/agentscope-go/pkg/model/xai"
	"github.com/yuluo-yx/agentscope-go/pkg/model/zhipu"
)

type providerSmokeSpec struct {
	Name         string
	Description  string
	EnableEnv    string
	KeyEnvNames  []string
	ModelEnv     string
	DefaultModel string
	NewModel     func(apiKey, modelName string) (asmodel.ChatModel, error)
}

func init() {
	registerProviderSmoke(providerSmokeSpec{
		Name:         "provider-openai-chat-smoke",
		Description:  "OpenAI ChatModel live Call smoke test",
		EnableEnv:    "AGENTSCOPE_TEST_OPENAI",
		KeyEnvNames:  []string{"OPENAI_API_KEY"},
		ModelEnv:     "AGENTSCOPE_TEST_OPENAI_MODEL",
		DefaultModel: "gpt-4o-mini",
		NewModel: func(apiKey, modelName string) (asmodel.ChatModel, error) {
			return openai.NewChatModel(openai.NewCredential(apiKey), modelName, openai.WithMaxRetries(1), openai.WithStream(false))
		},
	})
	registerProviderSmoke(providerSmokeSpec{
		Name:         "provider-openai-responses-smoke",
		Description:  "OpenAI ResponsesModel live Call smoke test",
		EnableEnv:    "AGENTSCOPE_TEST_OPENAI_RESPONSES",
		KeyEnvNames:  []string{"OPENAI_API_KEY"},
		ModelEnv:     "AGENTSCOPE_TEST_OPENAI_RESPONSES_MODEL",
		DefaultModel: "gpt-5.4",
		NewModel: func(apiKey, modelName string) (asmodel.ChatModel, error) {
			return openairesponse.NewResponseModel(openairesponse.NewCredential(apiKey), modelName)
		},
	})
	registerProviderSmoke(providerSmokeSpec{
		Name:         "provider-dashscope-chat-smoke",
		Description:  "DashScope ChatModel live Call smoke test",
		EnableEnv:    "AGENTSCOPE_TEST_DASHSCOPE",
		KeyEnvNames:  []string{"DASHSCOPE_API_KEY", "AI_DASHSCOPE_API_KEY"},
		ModelEnv:     "AGENTSCOPE_TEST_DASHSCOPE_MODEL",
		DefaultModel: "qwen-plus",
		NewModel: func(apiKey, modelName string) (asmodel.ChatModel, error) {
			return dashscope.NewChatModel(dashscope.NewCredential(apiKey), modelName, dashscope.WithMaxRetries(1), dashscope.WithStream(false))
		},
	})
	registerProviderSmoke(providerSmokeSpec{
		Name:         "provider-deepseek-chat-smoke",
		Description:  "DeepSeek ChatModel live Call smoke test",
		EnableEnv:    "AGENTSCOPE_TEST_DEEPSEEK",
		KeyEnvNames:  []string{"DEEPSEEK_API_KEY"},
		ModelEnv:     "AGENTSCOPE_TEST_DEEPSEEK_MODEL",
		DefaultModel: "deepseek-chat",
		NewModel: func(apiKey, modelName string) (asmodel.ChatModel, error) {
			return deepseek.NewChatModel(deepseek.NewCredential(apiKey), modelName, deepseek.WithMaxRetries(1), deepseek.WithStream(false))
		},
	})
	registerProviderSmoke(providerSmokeSpec{
		Name:         "provider-gemini-chat-smoke",
		Description:  "Gemini ChatModel live Call smoke test",
		EnableEnv:    "AGENTSCOPE_TEST_GEMINI",
		KeyEnvNames:  []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
		ModelEnv:     "AGENTSCOPE_TEST_GEMINI_MODEL",
		DefaultModel: "gemini-2.5-flash",
		NewModel: func(apiKey, modelName string) (asmodel.ChatModel, error) {
			return gemini.NewChatModel(gemini.NewCredential(apiKey), modelName)
		},
	})
	registerProviderSmoke(providerSmokeSpec{
		Name:         "provider-moonshot-chat-smoke",
		Description:  "Moonshot ChatModel live Call smoke test",
		EnableEnv:    "AGENTSCOPE_TEST_MOONSHOT",
		KeyEnvNames:  []string{"MOONSHOT_API_KEY", "KIMI_API_KEY"},
		ModelEnv:     "AGENTSCOPE_TEST_MOONSHOT_MODEL",
		DefaultModel: "moonshot-v1-8k",
		NewModel: func(apiKey, modelName string) (asmodel.ChatModel, error) {
			return moonshot.NewChatModel(moonshot.NewCredential(apiKey), modelName, moonshot.WithMaxRetries(1), moonshot.WithStream(false))
		},
	})
	registerProviderSmoke(providerSmokeSpec{
		Name:         "provider-xai-chat-smoke",
		Description:  "xAI ChatModel live Call smoke test",
		EnableEnv:    "AGENTSCOPE_TEST_XAI",
		KeyEnvNames:  []string{"XAI_API_KEY"},
		ModelEnv:     "AGENTSCOPE_TEST_XAI_MODEL",
		DefaultModel: "grok-3",
		NewModel: func(apiKey, modelName string) (asmodel.ChatModel, error) {
			return xai.NewChatModel(xai.NewCredential(apiKey), modelName, xai.WithMaxRetries(1), xai.WithStream(false))
		},
	})
	registerProviderSmoke(providerSmokeSpec{
		Name:         "provider-zhipu-chat-smoke",
		Description:  "Zhipu ChatModel live Call smoke test",
		EnableEnv:    "AGENTSCOPE_TEST_ZHIPU",
		KeyEnvNames:  []string{"ZHIPU_API_KEY", "ZHIPUAI_API_KEY", "BIGMODEL_API_KEY"},
		ModelEnv:     "AGENTSCOPE_TEST_ZHIPU_MODEL",
		DefaultModel: "glm-4.5-flash",
		NewModel: func(apiKey, modelName string) (asmodel.ChatModel, error) {
			return zhipu.NewChatModel(zhipu.NewCredential(apiKey), modelName, zhipu.WithMaxRetries(1), zhipu.WithStream(false))
		},
	})
}

func registerProviderSmoke(spec providerSmokeSpec) {
	pkgtestcases.Register(spec.Name, pkgtestcases.TestCase{
		Description: spec.Description,
		Tags:        []string{"provider", "live", "smoke"},
		Fn: func(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
			return runProviderSmoke(ctx, opts, spec)
		},
	})
}

func runProviderSmoke(ctx context.Context, opts pkgtestcases.TestCaseOptions, spec providerSmokeSpec) error {
	if os.Getenv(spec.EnableEnv) != "1" {
		return framework.Skipf("set %s=1 to run this real provider smoke test", spec.EnableEnv)
	}
	apiKey, err := requireAnyProviderEnv(spec.KeyEnvNames...)
	if err != nil {
		return err
	}
	modelName := envOrDefault(spec.ModelEnv, spec.DefaultModel)
	model, err := spec.NewModel(apiKey, modelName)
	if err != nil {
		return fmt.Errorf("%s model initialization failed: %w", spec.Name, err)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model": modelName, "provider_case": spec.Name})
	}
	return assertProviderSmokeResponse(ctx, opts, model)
}

func assertProviderSmokeResponse(ctx context.Context, opts pkgtestcases.TestCaseOptions, model asmodel.ChatModel) error {
	testCtx, cancel := context.WithTimeout(ctx, liveCaseTimeout(opts.Timeout))
	defer cancel()

	userMsg, err := message.NewUserMessage("provider-smoke", "Reply with the single word ok.")
	if err != nil {
		return err
	}
	resp, err := model.Call(testCtx, asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		return fmt.Errorf("%s Call returned error: %w", model.Name(), err)
	}
	if resp == nil || len(resp.Content) == 0 {
		return fmt.Errorf("%s returned empty smoke response: %#v", model.Name(), resp)
	}
	return nil
}

func requireAnyProviderEnv(names ...string) (string, error) {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("one of %v must be set", names)
}
