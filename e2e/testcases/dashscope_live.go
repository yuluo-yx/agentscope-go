package testcases

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	dashscopeembedding "github.com/yuluo-yx/agentscope-go/embedding/dashscope"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/types"
)

func init() {
	pkgtestcases.Register("dashscope-chat-call", pkgtestcases.TestCase{
		Description: "DashScope live ChatModel Call returns normalized content and token metadata",
		Tags:        []string{"dashscope-live", "model", "call"},
		Fn:          testDashScopeChatCall,
	})
	pkgtestcases.Register("dashscope-chat-stream", pkgtestcases.TestCase{
		Description: "DashScope live ChatModel Stream emits terminal responses through the unified contract",
		Tags:        []string{"dashscope-live", "model", "stream"},
		Fn:          testDashScopeChatStream,
	})
	pkgtestcases.Register("dashscope-agent-tool-loop", pkgtestcases.TestCase{
		Description: "DashScope live Agent loop calls a local function tool and consumes the tool result",
		Tags:        []string{"dashscope-live", "agent", "tool"},
		Fn:          testDashScopeAgentToolLoop,
	})
	pkgtestcases.Register("dashscope-agent-events", pkgtestcases.TestCase{
		Description: "DashScope live Agent emits model, tool, text, and reply events in one ReAct loop",
		Tags:        []string{"dashscope-live", "agent", "events"},
		Fn:          testDashScopeAgentEvents,
	})
	pkgtestcases.Register("dashscope-embedding-text", pkgtestcases.TestCase{
		Description: "DashScope live text embedding returns normalized vectors and usage metadata",
		Tags:        []string{"dashscope-live", "embedding", "text"},
		Fn:          testDashScopeTextEmbedding,
	})
}

func testDashScopeChatCall(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	model, modelName, err := newDashScopeLiveModel(false)
	if err != nil {
		return err
	}
	testCtx, cancel := context.WithTimeout(ctx, liveCaseTimeout(opts.Timeout))
	defer cancel()

	userMsg, err := message.NewUserMessage("e2e", "Reply with the single word ok.")
	if err != nil {
		return err
	}
	request := modelpkg.CallRequest{Messages: []*message.Message{userMsg}}
	tokenCount, err := model.CountTokens(request)
	if err != nil {
		return err
	}
	if tokenCount <= 0 {
		return fmt.Errorf("expected positive DashScope token count, got %d", tokenCount)
	}
	response, err := model.Call(testCtx, request)
	if err != nil {
		return fmt.Errorf("%s Call failed: %w", model.Name(), err)
	}
	if response == nil || !response.Content.HasContentBlocks("text") {
		return fmt.Errorf("%s returned empty non-text response: %#v", model.Name(), response)
	}
	text := strings.TrimSpace(valueOrEmpty(response.GetTextContent("")))
	if text == "" {
		return fmt.Errorf("%s returned blank text response: %#v", model.Name(), response)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{
			"model":        modelName,
			"provider":     model.Name(),
			"token_count":  tokenCount,
			"response_len": len(text),
		})
	}
	return nil
}

func testDashScopeChatStream(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	model, modelName, err := newDashScopeLiveModel(true)
	if err != nil {
		return err
	}
	testCtx, cancel := context.WithTimeout(ctx, liveCaseTimeout(opts.Timeout))
	defer cancel()

	userMsg, err := message.NewUserMessage("e2e", "Reply with one short sentence confirming stream works.")
	if err != nil {
		return err
	}
	stream, err := model.Stream(testCtx, modelpkg.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		return fmt.Errorf("%s Stream failed before receiving chunks: %w", model.Name(), err)
	}
	chunks := collectChatStream(stream)
	if len(chunks) == 0 {
		return fmt.Errorf("%s Stream returned no chunks", model.Name())
	}
	var final *modelpkg.ChatResponse
	textChunks := 0
	for i := range chunks {
		chunk := chunks[i].Clone()
		if chunk.Error != nil {
			return fmt.Errorf("%s Stream terminal error: %w", model.Name(), chunk.Error)
		}
		if strings.TrimSpace(valueOrEmpty(chunk.GetTextContent(""))) != "" {
			textChunks++
		}
		if chunk.IsLast {
			final = chunk
		}
	}
	if final == nil {
		return fmt.Errorf("%s Stream did not emit an IsLast response", model.Name())
	}
	if text := strings.TrimSpace(valueOrEmpty(final.GetTextContent(""))); text == "" {
		return fmt.Errorf("%s Stream final response had blank text: %#v", model.Name(), final)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{
			"model":       modelName,
			"provider":    model.Name(),
			"chunks":      len(chunks),
			"text_chunks": textChunks,
		})
	}
	return nil
}

func testDashScopeAgentToolLoop(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	result, err := runDashScopeLiveAgent(ctx, opts)
	if err != nil {
		return err
	}
	if result.toolCalls == 0 {
		return fmt.Errorf("DashScope Agent did not execute ResolveName")
	}
	if result.toolResult == nil || result.toolResult.State != message.ToolResultSuccess {
		return fmt.Errorf("expected successful ResolveName tool result, got %#v", result.toolResult)
	}
	if strings.TrimSpace(result.finalText) == "" {
		return fmt.Errorf("DashScope Agent returned blank final text")
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{
			"model":        result.modelName,
			"provider":     result.providerName,
			"model_calls":  result.modelCalls,
			"tool_calls":   result.toolCalls,
			"events":       len(result.events),
			"final_length": len(result.finalText),
		})
	}
	return nil
}

func testDashScopeAgentEvents(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	result, err := runDashScopeLiveAgent(ctx, opts)
	if err != nil {
		return err
	}
	if err := assertEventOrder(result.events, message.ModelCallStartType, message.ToolCallStartType, message.ToolResultEndType, message.TextBlockDeltaType, message.ReplyEndType); err != nil {
		return err
	}
	if result.modelCalls < 2 {
		return fmt.Errorf("expected at least two model calls around the tool result, got %d", result.modelCalls)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{
			"model":       result.modelName,
			"provider":    result.providerName,
			"model_calls": result.modelCalls,
			"tool_calls":  result.toolCalls,
			"events":      eventTypeNames(result.events),
		})
	}
	return nil
}

func testDashScopeTextEmbedding(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	apiKey := dashScopeLiveAPIKey()
	if apiKey == "" {
		return fmt.Errorf("set DASHSCOPE_API_KEY or AI_DASHSCOPE_API_KEY to run DashScope live E2E tests")
	}
	modelName := dashScopeLiveEmbeddingModelName()
	model, err := dashscopeembedding.NewTextModel(dashscopeembedding.NewCredential(apiKey), modelName)
	if err != nil {
		return err
	}
	testCtx, cancel := context.WithTimeout(ctx, liveCaseTimeout(opts.Timeout))
	defer cancel()

	response, err := model.Embed(testCtx, asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{
			asembedding.NewTextInput("AgentScope Go DashScope text embedding e2e."),
			asembedding.NewTextInput("A second deterministic input keeps batching observable."),
		},
	})
	if err != nil {
		return fmt.Errorf("%s Embed failed: %w", model.Name(), err)
	}
	if response == nil || len(response.Embeddings) != 2 {
		return fmt.Errorf("%s returned unexpected embedding count: %#v", model.Name(), response)
	}
	for index, embedding := range response.Embeddings {
		if len(embedding) != model.Dimensions() {
			return fmt.Errorf("%s embedding %d dimension mismatch: got %d want %d", model.Name(), index, len(embedding), model.Dimensions())
		}
	}
	if response.Usage == nil || response.Usage.Type != asembedding.UsageTypeEmbedding {
		return fmt.Errorf("%s returned missing embedding usage: %#v", model.Name(), response.Usage)
	}
	if opts.SetDetails != nil {
		tokenCount := 0
		if response.Usage.Tokens != nil {
			tokenCount = *response.Usage.Tokens
		}
		opts.SetDetails(map[string]any{
			"model":      modelName,
			"provider":   model.Name(),
			"dimensions": model.Dimensions(),
			"embeddings": len(response.Embeddings),
			"tokens":     tokenCount,
		})
	}
	return nil
}

type dashScopeAgentRun struct {
	modelName    string
	providerName string
	finalText    string
	events       []message.Event
	toolCalls    int
	modelCalls   int
	toolResult   *message.ToolResultBlock
}

func runDashScopeLiveAgent(ctx context.Context, opts pkgtestcases.TestCaseOptions) (*dashScopeAgentRun, error) {
	model, modelName, err := newDashScopeLiveModel(true)
	if err != nil {
		return nil, err
	}
	testCtx, cancel := context.WithTimeout(ctx, liveCaseTimeout(opts.Timeout))
	defer cancel()

	var toolCalls int
	var toolMu sync.Mutex
	resolve, err := tool.NewFunctionTool(
		"ResolveName",
		"Resolve one short person name into the canonical e2e answer.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Name to resolve."},
			},
			"required": []string{"name"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			toolMu.Lock()
			toolCalls++
			toolMu.Unlock()
			name := strings.TrimSpace(fmt.Sprint(input["name"]))
			if name == "" || name == "<nil>" {
				name = "Ada"
			}
			return message.ContentBlockList{message.NewTextBlock("resolved:" + name + ":Ada Lovelace")}, nil
		},
		tool.WithFunctionReadOnly(true),
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{
				Behavior:       permission.BehaviorAllow,
				Message:        "ResolveName allowed for DashScope live E2E",
				DecisionReason: "E2E test tool is local and deterministic",
			}, nil
		}),
	)
	if err != nil {
		return nil, err
	}
	kit, err := tool.NewToolkit(resolve)
	if err != nil {
		return nil, err
	}
	choice := &dashScopeToolChoiceMiddleware{toolName: resolve.Name()}
	agent, err := agentpkg.NewAgent(
		"DashScopeE2E",
		"Use ResolveName exactly once when asked to resolve Ada. After the tool result is available, do not call tools again and answer briefly with the result.",
		model,
		agentpkg.WithToolkit(kit),
		agentpkg.WithMiddlewares(choice),
		agentpkg.WithModelConfig(agentpkg.ModelConfig{MaxRetries: 1}),
		agentpkg.WithReActConfig(agentpkg.ReActConfig{MaxIters: 4}),
	)
	if err != nil {
		return nil, err
	}
	userMsg, err := message.NewUserMessage("e2e", "Resolve Ada with the ResolveName tool, then answer with the tool result in one short sentence.")
	if err != nil {
		return nil, err
	}

	var events []message.Event
	if err := agent.ReplyStream(testCtx, userMsg, func(evt message.Event) error {
		events = append(events, evt)
		return nil
	}); err != nil {
		return nil, err
	}
	state := agent.AgentState()
	if state == nil || len(state.Context) == 0 {
		return nil, fmt.Errorf("agent state has no final context")
	}
	final := state.Context[len(state.Context)-1]
	finalText := strings.TrimSpace(valueOrEmpty(final.GetTextContent("")))
	var toolResult *message.ToolResultBlock
	for _, block := range final.GetContentBlocks("tool_result") {
		result, ok := block.(*message.ToolResultBlock)
		if ok {
			copy := *result
			copy.Output.Blocks = result.Output.Blocks.Clone()
			toolResult = &copy
		}
	}
	toolMu.Lock()
	calls := toolCalls
	toolMu.Unlock()
	return &dashScopeAgentRun{
		modelName:    modelName,
		providerName: model.Name(),
		finalText:    finalText,
		events:       events,
		toolCalls:    calls,
		modelCalls:   choice.ModelCalls(),
		toolResult:   toolResult,
	}, nil
}

type dashScopeToolChoiceMiddleware struct {
	toolName string
	mu       sync.Mutex
	calls    int
}

func (m *dashScopeToolChoiceMiddleware) MiddlewareName() string { return "dashscope-live-tool-choice" }

func (m *dashScopeToolChoiceMiddleware) OnModelCall(
	ctx context.Context,
	_ agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ModelCallHandler,
) (<-chan modelpkg.ChatResponse, error) {
	request, ok := input["request"].(modelpkg.CallRequest)
	if !ok {
		return nil, fmt.Errorf("dashscope-live-tool-choice: missing model request in hook input")
	}
	m.mu.Lock()
	m.calls++
	isFirstCall := m.calls == 1
	m.mu.Unlock()
	if isFirstCall {
		choice, err := types.NewToolChoice(m.toolName, m.toolName)
		if err != nil {
			return nil, err
		}
		request.ToolChoice = choice
	} else {
		choice, err := types.NewToolChoice(string(types.ToolChoiceNone))
		if err != nil {
			return nil, err
		}
		request.ToolChoice = choice
	}
	input["request"] = request
	return next(ctx)
}

func (m *dashScopeToolChoiceMiddleware) ModelCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func newDashScopeLiveModel(stream bool) (modelpkg.ChatModel, string, error) {
	apiKey := dashScopeLiveAPIKey()
	if apiKey == "" {
		return nil, "", fmt.Errorf("set DASHSCOPE_API_KEY or AI_DASHSCOPE_API_KEY to run DashScope live E2E tests")
	}
	modelName := dashScopeLiveModelName()
	maxTokens := int64(256)
	temperature := 0.0
	model, err := dashscope.NewChatModel(
		dashscope.NewCredential(apiKey),
		modelName,
		dashscope.WithMaxRetries(1),
		dashscope.WithStream(stream),
		dashscope.WithChatParameters(dashscope.ChatParameters{
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
		}),
	)
	if err != nil {
		return nil, "", err
	}
	return model, modelName, nil
}

func dashScopeLiveAPIKey() string {
	if value := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("AI_DASHSCOPE_API_KEY"))
}

func dashScopeLiveModelName() string {
	if value := strings.TrimSpace(os.Getenv("AGENTSCOPE_TEST_DASHSCOPE_MODEL")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("AI_DASHSCOPE_MODEL")); value != "" {
		return value
	}
	return "qwen-plus"
}

func dashScopeLiveEmbeddingModelName() string {
	if value := strings.TrimSpace(os.Getenv("AGENTSCOPE_TEST_DASHSCOPE_EMBEDDING_MODEL")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("AI_DASHSCOPE_EMBEDDING_MODEL")); value != "" {
		return value
	}
	return "text-embedding-v4"
}

func liveCaseTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 && timeout < 60*time.Second {
		return timeout
	}
	return 60 * time.Second
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func eventTypeNames(events []message.Event) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, string(event.GetType()))
	}
	return names
}
