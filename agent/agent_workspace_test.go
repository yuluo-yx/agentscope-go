package agent_test

import (
	"context"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	"github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/tool/skill"
	"github.com/yuluo-yx/agentscope-go/workspace"
)

func TestAgentWithWorkspaceWiresResourcesAndOffloaderLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	largeTool, err := tool.NewFunctionTool(
		"LargeWorkspaceTool",
		"Returns a large workspace result.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *state.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("abcdefghijklmnopqrstuvwxyz")}, nil
		},
		tool.WithFunctionReadOnly(true),
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorAllow}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	ws := &agentWorkspace{
		instructions: "<workspace>Use /tmp/workspace.</workspace>",
		tools:        []workspace.Tool{largeTool},
		skills: []skill.Skill{{
			Name:        "review",
			Description: "Review code",
			Dir:         "/tmp/workspace/skills/review",
			Markdown:    "Review carefully.",
		}},
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{
			message.NewToolCallBlock("call-large", "LargeWorkspaceTool", `{}`),
		}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("done")}, true),
	}}
	config := agentpkg.DefaultContextConfig()
	config.ToolResultLimit = 8
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Base prompt.",
		model,
		agentpkg.WithContextConfig(config),
		agentpkg.WithWorkspace(ctx, ws),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	user, err := message.NewUserMessage("user", "Run it")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	if _, err := agent.Reply(ctx, user); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}
	if len(model.requests) == 0 {
		t.Fatalf("model should receive at least one request")
	}
	systemText := model.requests[0].Messages[0].Content.GetTextContent("")
	if systemText == nil ||
		!strings.Contains(*systemText, "Base prompt.") ||
		!strings.Contains(*systemText, "Use /tmp/workspace") ||
		!strings.Contains(*systemText, "<name>review</name>") {
		t.Fatalf("system prompt should include base, workspace and skills: %v", systemText)
	}
	if got := requestToolNames(model.requests[0]); strings.Join(got, ",") != "LargeWorkspaceTool,Skill" {
		t.Fatalf("workspace tools and skill viewer should be exposed: %#v", got)
	}
	if len(ws.offloadedToolResults) != 1 {
		t.Fatalf("expected one offloaded tool result, got %d", len(ws.offloadedToolResults))
	}
	offloadedText := ws.offloadedToolResults[0].Output.Blocks.GetTextContent("")
	if offloadedText == nil || *offloadedText != "ijklmnopqrstuvwxyz" {
		t.Fatalf("offloaded tool result should contain truncated tail: %v", offloadedText)
	}
	reserved := firstToolResult(agent.AgentState().Context)
	if reserved == nil {
		t.Fatalf("agent state should keep reserved tool result")
	}
	reservedText := reserved.Output.Blocks.GetTextContent("")
	if reservedText == nil ||
		!strings.Contains(*reservedText, "abcdefgh") ||
		!strings.Contains(*reservedText, "offloaded to") ||
		!strings.Contains(*reservedText, "workspace://tool-results/call-large.txt") {
		t.Fatalf("reserved tool result should include truncated head and offload reminder: %v", reservedText)
	}
}

type agentWorkspace struct {
	instructions         string
	tools                []workspace.Tool
	skills               []skill.Skill
	offloadedToolResults []*message.ToolResultBlock
}

func (w *agentWorkspace) WorkspaceID() string              { return "agent-workspace" }
func (w *agentWorkspace) IsAlive() bool                    { return true }
func (w *agentWorkspace) Initialize(context.Context) error { return nil }
func (w *agentWorkspace) Close(context.Context) error      { return nil }
func (w *agentWorkspace) Reset(context.Context) error      { return nil }
func (w *agentWorkspace) GetInstructions(context.Context) (string, error) {
	return w.instructions, nil
}
func (w *agentWorkspace) ListTools(context.Context) ([]workspace.Tool, error) {
	return append([]workspace.Tool(nil), w.tools...), nil
}
func (w *agentWorkspace) ListMCPs(context.Context) ([]workspace.MCPClient, error) {
	return []workspace.MCPClient{}, nil
}
func (w *agentWorkspace) ListSkills(context.Context) ([]skill.Skill, error) {
	return append([]skill.Skill(nil), w.skills...), nil
}
func (w *agentWorkspace) OffloadContext(context.Context, string, []*message.Message) (string, error) {
	return "workspace://context/context.jsonl", nil
}
func (w *agentWorkspace) OffloadToolResult(_ context.Context, _ string, result *message.ToolResultBlock) (string, error) {
	w.offloadedToolResults = append(w.offloadedToolResults, result.Clone().(*message.ToolResultBlock))
	return "workspace://tool-results/" + result.ID + ".txt", nil
}
func (w *agentWorkspace) OffloadDataBlock(context.Context, *message.DataBlock) (*message.DataBlock, error) {
	return nil, nil
}
func (w *agentWorkspace) AddMCP(context.Context, workspace.MCPClient) error { return nil }
func (w *agentWorkspace) RemoveMCP(context.Context, string) error           { return nil }
func (w *agentWorkspace) AddSkill(context.Context, string) error            { return nil }
func (w *agentWorkspace) RemoveSkill(context.Context, string) error         { return nil }

func requestToolNames(request modelpkg.CallRequest) []string {
	names := make([]string, 0, len(request.Tools))
	for _, schema := range request.Tools {
		names = append(names, schema.Function.Name)
	}
	return names
}

func firstToolResult(messages []*message.Message) *message.ToolResultBlock {
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		for _, block := range msg.GetContentBlocks("tool_result") {
			if result, ok := block.(*message.ToolResultBlock); ok {
				return result
			}
		}
	}
	return nil
}

var _ workspace.Workspace = (*agentWorkspace)(nil)
