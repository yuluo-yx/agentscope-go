// Copyright The AgentScope Go Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package agent_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
)

func TestAgentObserveAppendsExternalMessages(t *testing.T) {
	t.Parallel()

	agent, err := agentpkg.NewAgent("Friday", "Observe.", &scriptedChatModel{})
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	msg, err := message.NewAssistantMessage(
		"service",
		message.ContentBlockList{message.NewHintBlock("deployment finished")},
	)
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}

	if err := agent.Observe(context.Background(), msg); err != nil {
		t.Fatalf("Observe returned error: %v", err)
	}
	msg.Content[0].(*message.HintBlock).Hint = "mutated"

	state := agent.AgentState()
	if len(state.Context) != 1 {
		t.Fatalf("Observe should append one external message, got %d", len(state.Context))
	}
	hints := state.Context[0].GetContentBlocks("hint")
	if len(hints) != 1 || hints[0].(*message.HintBlock).Hint != "deployment finished" {
		t.Fatalf("Observe should store a cloned hint message, got %#v", state.Context[0].Content)
	}
}

func TestAgentObserveReplaysConfirmationAndReplyResumes(t *testing.T) {
	t.Parallel()

	executed := false
	write, err := tool.NewFunctionTool(
		"WriteThing",
		"Write a value.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *statepkg.AgentState) (message.ContentBlockList, error) {
			executed = true
			return message.ContentBlockList{message.NewTextBlock("written")}, nil
		},
		tool.WithFunctionSuggestedRule("WriteThing"),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	kit, err := tool.NewToolkit(write)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("call-ask", "WriteThing", `{}`)},
			true,
		),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("done")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Ask before writes.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Write")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	var confirm *message.RequireUserConfirmEvent
	replyErr := agent.ReplyStream(context.Background(), userMsg, func(evt message.Event) error {
		if e, ok := evt.(*message.RequireUserConfirmEvent); ok {
			confirm = e
		}
		return nil
	})
	if replyErr != nil {
		t.Fatalf("initial ReplyStream returned error: %v", replyErr)
	}
	if confirm == nil || len(confirm.ToolCalls) != 1 {
		t.Fatalf("expected confirmation event, got %#v", confirm)
	}

	confirmEvent := message.NewUserConfirmResultEvent(confirm.ReplyID(), []message.ConfirmResult{{
		Confirmed: true,
		ToolCall:  confirm.ToolCalls[0],
		Rules:     confirm.ToolCalls[0].SuggestedRules,
	}})
	observeErr := agent.Observe(context.Background(), confirmEvent)
	if observeErr != nil {
		t.Fatalf("Observe confirmation returned error: %v", observeErr)
	}
	reply, err := agent.Reply(context.Background(), nil)
	if err != nil {
		t.Fatalf("resume Reply returned error: %v", err)
	}
	if !executed {
		t.Fatal("observed confirmation should let Reply resume the pending tool call")
	}
	if text := reply.GetTextContent(""); text == nil || *text != "done" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
}

func TestCompressContextPreservesPendingToolCallsTasksAndCleansReadCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	recentPath := filepath.Join(dir, "recent.txt")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.WriteFile(recentPath, []byte("recent"), 0o600); err != nil {
		t.Fatalf("write recent file: %v", err)
	}

	state := statepkg.NewAgentState()
	if err := state.ToolContext.CacheFile(oldPath, []string{"old"}); err != nil {
		t.Fatalf("CacheFile old returned error: %v", err)
	}
	if err := state.ToolContext.CacheFile(recentPath, []string{"recent"}); err != nil {
		t.Fatalf("CacheFile recent returned error: %v", err)
	}
	task := statepkg.NewTask("ship observe", "keep pending tasks through compression", nil)
	state.TaskContext.AddTask(task)

	oldRead := mustAssistantMessage(t, "Friday", message.ContentBlockList{
		message.NewTextBlock(strings.Repeat("old context ", 80)),
		message.NewToolCallBlock("read-old", "Read", fmt.Sprintf(`{"file_path":%q}`, oldPath), message.WithToolCallState(message.ToolCallFinished)),
		message.NewToolResultBlock("read-old", "Read", message.ToolResultOutput{Raw: "old"}, message.ToolResultSuccess),
	})
	pending := mustAssistantMessage(t, "Friday", message.ContentBlockList{
		message.NewToolCallBlock("call-ask", "WriteThing", `{}`, message.WithToolCallState(message.ToolCallAsking)),
	})
	recentRead := mustAssistantMessage(t, "Friday", message.ContentBlockList{
		message.NewToolCallBlock("read-recent", "Read", fmt.Sprintf(`{"file_path":%q}`, recentPath), message.WithToolCallState(message.ToolCallFinished)),
		message.NewToolResultBlock("read-recent", "Read", message.ToolResultOutput{Raw: "recent"}, message.ToolResultSuccess),
	})
	observed := mustUserMessage(t, "service", "external hint after interrupt")
	state.Context = []*message.Message{oldRead, pending, recentRead, observed}

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock(`{
			"task_overview": "old context compressed",
			"current_state": "waiting for confirmation",
			"important_discoveries": "recent read remains",
			"next_steps": "resume pending call",
			"context_to_preserve": "permission interrupt"
		}`)}, true),
	}}
	config := agentpkg.DefaultContextConfig()
	config.MaxTokens = 2
	config.ToolResultLimit = 10000
	offloader := &recordingContextOffloader{}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Compress safely.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithContextConfig(config),
		agentpkg.WithOffloader(offloader),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}

	if err := agent.CompressContext(ctx); err != nil {
		t.Fatalf("CompressContext returned error: %v", err)
	}

	if !containsToolCall(state.Context, "call-ask", message.ToolCallAsking) {
		t.Fatalf("pending permission interrupt should remain in context: %#v", state.Context)
	}
	if containsToolCall(state.Context, "read-old", message.ToolCallFinished) {
		t.Fatalf("old read call should be summarized out of active context")
	}
	if !containsToolCall(state.Context, "read-recent", message.ToolCallFinished) {
		t.Fatalf("reserved read call should remain in active context")
	}
	if got, ok := state.TaskContext.GetTask(task.ID); !ok || got.State != statepkg.TaskPending {
		t.Fatalf("pending task should survive compression, got %#v ok=%t", got, ok)
	}
	if len(offloader.contexts) != 1 || len(offloader.contexts[0]) != 1 {
		t.Fatalf("compressed context should be offloaded once with old messages, got %#v", offloader.contexts)
	}
	if !strings.Contains(state.Summary.Text, "workspace://context/summary.jsonl") {
		t.Fatalf("summary should reference offloaded context, got %q", state.Summary.Text)
	}
	if got := cachedPaths(state.ToolContext); strings.Join(got, ",") != recentPath {
		t.Fatalf("read cache should keep only reserved Read paths, got %#v", got)
	}
}

type recordingContextOffloader struct {
	contexts [][]*message.Message
}

func (o *recordingContextOffloader) OffloadContext(_ context.Context, _ string, messages []*message.Message) (string, error) {
	o.contexts = append(o.contexts, cloneMessagesForTest(messages))
	return "workspace://context/summary.jsonl", nil
}

func (o *recordingContextOffloader) OffloadToolResult(context.Context, string, *message.ToolResultBlock) (string, error) {
	return "workspace://tool-result/unused.txt", nil
}

func (o *recordingContextOffloader) OffloadDataBlock(context.Context, *message.DataBlock) (*message.DataBlock, error) {
	return nil, nil
}

func mustAssistantMessage(t *testing.T, name string, content message.ContentBlockList) *message.Message {
	t.Helper()
	msg, err := message.NewAssistantMessage(name, content)
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	return msg
}

func mustUserMessage(t *testing.T, name string, content any) *message.Message {
	t.Helper()
	msg, err := message.NewUserMessage(name, content)
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	return msg
}

func containsToolCall(messages []*message.Message, id string, state message.ToolCallState) bool {
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

func cachedPaths(toolContext *statepkg.ToolContext) []string {
	if toolContext == nil {
		return nil
	}
	paths := make([]string, 0, len(toolContext.ReadFileCache))
	for _, entry := range toolContext.ReadFileCache {
		paths = append(paths, entry.FilePath)
	}
	return paths
}

func cloneMessagesForTest(messages []*message.Message) []*message.Message {
	out := make([]*message.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, msg.Clone())
	}
	return out
}
