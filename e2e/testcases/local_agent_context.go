package testcases

import (
	"context"
	"fmt"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func init() {
	pkgtestcases.Register("agent-observe-permission-context", pkgtestcases.TestCase{
		Description: "Agent Observe, permission confirmation, resume, and context compression compose in one local E2E flow",
		Tags:        []string{"local", "agent", "permission", "context"},
		Fn:          testAgentObservePermissionContext,
	})
}

func testAgentObservePermissionContext(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	if err := exerciseObservePermissionResume(ctx); err != nil {
		return err
	}
	if err := exercisePendingPermissionContextCompression(ctx); err != nil {
		return err
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"flows": []string{"observe-confirm-resume", "pending-permission-compression"}})
	}
	return nil
}

func exerciseObservePermissionResume(ctx context.Context) error {
	executed := false
	write, err := tool.NewFunctionTool(
		"WriteThing",
		"Write a value after confirmation.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			executed = true
			return message.ContentBlockList{message.NewTextBlock("written")}, nil
		},
		tool.WithFunctionSuggestedRule("WriteThing"),
	)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(write)
	if err != nil {
		return err
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("call-observe-ask", "WriteThing", `{}`)}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("observed confirmation resumed")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Ask before writes.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Write the release note")
	if err != nil {
		return err
	}

	var confirm *message.RequireUserConfirmEvent
	var events []message.Event
	if err := agent.ReplyStream(ctx, userMsg, func(evt message.Event) error {
		events = append(events, evt)
		if e, ok := evt.(*message.RequireUserConfirmEvent); ok {
			confirm = e
		}
		return nil
	}); err != nil {
		return err
	}
	if executed {
		return fmt.Errorf("WriteThing executed before user confirmation")
	}
	if confirm == nil || len(confirm.ToolCalls) != 1 {
		return fmt.Errorf("expected one user confirmation event, got %#v", confirm)
	}
	if err := assertEventOrder(events, message.ToolCallStartType, message.RequireUserConfirmType, message.ReplyEndType); err != nil {
		return err
	}
	if !containsToolCallState(agent.AgentState().Context, "call-observe-ask", message.ToolCallAsking) {
		return fmt.Errorf("agent context should contain asking tool call before observe confirmation")
	}

	confirmEvent := message.NewUserConfirmResultEvent(confirm.ReplyID(), []message.ConfirmResult{{
		Confirmed: true,
		ToolCall:  confirm.ToolCalls[0],
		Rules:     confirm.ToolCalls[0].SuggestedRules,
	}})
	if err := agent.Observe(ctx, confirmEvent); err != nil {
		return err
	}
	if !containsToolCallState(agent.AgentState().Context, "call-observe-ask", message.ToolCallAllowed) {
		return fmt.Errorf("observed confirmation should mark the tool call allowed")
	}
	if got := len(agent.AgentState().PermissionContext.AllowRules["WriteThing"]); got != 1 {
		return fmt.Errorf("observed confirmation should add one allow rule, got %d", got)
	}

	reply, err := agent.Reply(ctx, nil)
	if err != nil {
		return err
	}
	if !executed {
		return fmt.Errorf("Reply(nil) did not resume and execute the observed confirmation")
	}
	if text := reply.GetTextContent(""); text == nil || *text != "observed confirmation resumed" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}

	observed, err := message.NewAssistantMessage("service", message.ContentBlockList{message.NewHintBlock("deployment approved")})
	if err != nil {
		return err
	}
	if err := agent.Observe(ctx, observed); err != nil {
		return err
	}
	observed.Content[0].(*message.HintBlock).Hint = "mutated"
	if !containsHint(agent.AgentState().Context, "deployment approved") {
		return fmt.Errorf("observed message should be cloned into context: %#v", agent.AgentState().Context)
	}
	return nil
}

func exercisePendingPermissionContextCompression(ctx context.Context) error {
	state := asstate.NewAgentState()
	task := asstate.NewTask("resume after permission", "keep pending permission work through compression", nil)
	state.TaskContext.AddTask(task)

	oldUser, err := message.NewUserMessage("Tony", strings.Repeat("old conversation ", 80))
	if err != nil {
		return err
	}
	oldAssistant, err := message.NewAssistantMessage("Friday", strings.Repeat("old assistant context ", 80))
	if err != nil {
		return err
	}
	pending, err := message.NewAssistantMessage("Friday", message.ContentBlockList{
		message.NewToolCallBlock("call-compress-ask", "WriteThing", `{}`, message.WithToolCallState(message.ToolCallAsking)),
	})
	if err != nil {
		return err
	}
	observed, err := message.NewAssistantMessage("service", message.ContentBlockList{message.NewHintBlock("service approved later")})
	if err != nil {
		return err
	}
	state.Context = []*message.Message{oldUser, oldAssistant, pending, observed}

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock(`{
			"task_overview": "permission pause",
			"current_state": "waiting on user confirmation",
			"important_discoveries": "observed service hint must remain",
			"next_steps": "resume the allowed tool call",
			"context_to_preserve": "pending permission context"
		}`)}, true),
	}}
	config := agentpkg.DefaultContextConfig()
	config.MaxTokens = 2
	config.ToolResultLimit = 10000
	offloader := &e2eContextOffloader{contextPath: "workspace://context/observe-permission.jsonl"}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Compress permission context safely.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithContextConfig(config),
		agentpkg.WithOffloader(offloader),
	)
	if err != nil {
		return err
	}

	if err := agent.CompressContext(ctx); err != nil {
		return err
	}
	if !containsToolCallState(state.Context, "call-compress-ask", message.ToolCallAsking) {
		return fmt.Errorf("pending permission tool call should remain after compression: %#v", state.Context)
	}
	if !containsHint(state.Context, "service approved later") {
		return fmt.Errorf("observed service hint should remain after compression: %#v", state.Context)
	}
	if containsText(state.Context, "old conversation") || containsText(state.Context, "old assistant context") {
		return fmt.Errorf("old context should be summarized out of active context: %#v", state.Context)
	}
	if got, ok := state.TaskContext.GetTask(task.ID); !ok || got.State != asstate.TaskPending {
		return fmt.Errorf("pending task should survive compression, got %#v ok=%t", got, ok)
	}
	if len(offloader.contexts) != 1 || len(offloader.contexts[0]) == 0 {
		return fmt.Errorf("compressed context should be offloaded once, got %#v", offloader.contexts)
	}
	if !strings.Contains(state.Summary.Text, "permission pause") ||
		!strings.Contains(state.Summary.Text, "workspace://context/observe-permission.jsonl") {
		return fmt.Errorf("summary should include structured content and offload reminder: %s", state.Summary.Text)
	}
	return nil
}

type e2eContextOffloader struct {
	contextPath string
	contexts    [][]*message.Message
}

func (o *e2eContextOffloader) OffloadContext(_ context.Context, _ string, messages []*message.Message) (string, error) {
	cloned := make([]*message.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			cloned = append(cloned, msg.Clone())
		}
	}
	o.contexts = append(o.contexts, cloned)
	return o.contextPath, nil
}

func (o *e2eContextOffloader) OffloadToolResult(context.Context, string, *message.ToolResultBlock) (string, error) {
	return "workspace://tool-result/unused.txt", nil
}

func (o *e2eContextOffloader) OffloadDataBlock(_ context.Context, block *message.DataBlock) (*message.DataBlock, error) {
	if block == nil {
		return nil, nil
	}
	return block.Clone().(*message.DataBlock), nil
}

func containsToolCallState(messages []*message.Message, id string, state message.ToolCallState) bool {
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		for _, block := range msg.GetContentBlocks("tool_call") {
			toolCall, ok := block.(*message.ToolCallBlock)
			if ok && toolCall.ID == id && toolCall.State == state {
				return true
			}
		}
	}
	return false
}

func containsHint(messages []*message.Message, want string) bool {
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		for _, block := range msg.GetContentBlocks("hint") {
			hint, ok := block.(*message.HintBlock)
			if ok && strings.Contains(hint.Hint, want) {
				return true
			}
		}
	}
	return false
}

func containsText(messages []*message.Message, want string) bool {
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if text := msg.GetTextContent("\n"); text != nil && strings.Contains(*text, want) {
			return true
		}
	}
	return false
}
