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

package frameworke2e_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/middleware"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	wslocal "github.com/yuluo-yx/agentscope-go/workspace/local"
)

func TestFrameworkMiddlewareWorkspaceModelToolReActE2E(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	ws, err := wslocal.NewWorkspace(workdir, wslocal.WithWorkspaceID("middleware-react-e2e"))
	requireNoErr(t, err, "NewWorkspace returned error")
	recorder := &recordingTracer{}
	middlewareEcho, err := tool.NewFunctionTool(
		"MiddlewareEcho",
		"Echo a middleware-provided value.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []string{"value"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("middleware:" + input["value"].(string))}, nil
		},
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorAllow, Message: "allowed in e2e"}, nil
		}),
	)
	requireNoErr(t, err, "NewFunctionTool returned error")
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("middleware-call", "MiddlewareEcho", jsonInput(t, map[string]any{
				"value": "ready",
			}))},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("write-call", "Write", jsonInput(t, map[string]any{
				"file_path": filepath.Join(workdir, "notes.txt"),
				"content":   "middleware workspace note",
			}))},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("read-call", "Read", jsonInput(t, map[string]any{
				"file_path": filepath.Join(workdir, "notes.txt"),
				"limit":     5,
			}))},
			true,
		),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("middleware workspace verified")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["workspace"] = permission.AdditionalWorkingDirectory{Path: workdir, Source: "e2e"}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Use workspace tools.",
		model,
		agentpkg.WithWorkspace(ctx, ws),
		agentpkg.WithAgentState(state),
		agentpkg.WithMiddlewares(
			requestResponseMetadataMiddleware{},
			middleware.NewTracingMiddleware(recorder),
			middlewareToolList{tools: []agentpkg.Tool{middlewareEcho}},
		),
	)
	requireNoErr(t, err, "NewAgent returned error")
	userMsg, err := message.NewUserMessage("Tony", "Create and verify a workspace note")
	requireNoErr(t, err, "NewUserMessage returned error")

	reply, err := agent.Reply(ctx, userMsg)
	requireNoErr(t, err, "Reply returned error")

	if text := reply.GetTextContent(""); text == nil || *text != "middleware workspace verified" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
	data, err := os.ReadFile(filepath.Join(workdir, "notes.txt"))
	requireNoErr(t, err, "workspace file was not written")
	if string(data) != "middleware workspace note" {
		t.Fatalf("workspace file content mismatch: %q", string(data))
	}
	for index, request := range model.requests {
		if value, ok := request.Metadata["middleware_request"].(string); !ok || value != "preserved" {
			t.Fatalf("model request %d lost middleware metadata: %#v", index, request.Metadata)
		}
	}
	if !hasToolSchema(model.requests[0].Tools, "MiddlewareEcho") {
		t.Fatalf("middleware tool schema was not visible to model: %#v", model.requests[0].Tools)
	}
	middlewareResult := onlyToolResultFromRequest(t, model.requests[1])
	if text := middlewareResult.Output.Blocks.GetTextContent(""); middlewareResult.Name != "MiddlewareEcho" || text == nil || *text != "middleware:ready" {
		t.Fatalf("middleware tool result should remain visible to ReAct: %#v text=%#v", middlewareResult, text)
	}
	result := lastToolResultFromLastModelRequest(t, model)
	if text := result.Output.Blocks.GetTextContent(""); result.Name != "Read" || text == nil || !strings.Contains(*text, "middleware workspace note") {
		t.Fatalf("read tool result should remain visible to ReAct: %#v text=%#v", result, text)
	}
	names := recorder.SpanNames()
	assertSpanRecorded(t, names, "invoke_agent Friday")
	assertSpanRecorded(t, names, "chat scripted-framework-e2e")
	assertSpanRecorded(t, names, "execute_tool MiddlewareEcho")
	assertSpanRecorded(t, names, "execute_tool Write")
	assertSpanRecorded(t, names, "execute_tool Read")
}

type middlewareToolList struct {
	tools []agentpkg.Tool
}

func (m middlewareToolList) MiddlewareName() string {
	return "middleware-tool-list"
}

func (m middlewareToolList) ListTools(context.Context, agentpkg.AgentAccessor) ([]agentpkg.Tool, error) {
	return append([]agentpkg.Tool(nil), m.tools...), nil
}

func hasToolSchema(schemas []modelpkg.ToolSchema, name string) bool {
	for _, schema := range schemas {
		if schema.Function.Name == name {
			return true
		}
	}
	return false
}

func onlyToolResultFromRequest(t *testing.T, request modelpkg.CallRequest) *message.ToolResultBlock {
	t.Helper()
	if len(request.Messages) == 0 {
		t.Fatal("model request has no messages")
	}
	last := request.Messages[len(request.Messages)-1]
	results := last.GetContentBlocks("tool_result")
	if len(results) != 1 {
		t.Fatalf("expected one tool result, got %#v", last.Content)
	}
	return results[0].(*message.ToolResultBlock)
}

type requestResponseMetadataMiddleware struct{}

func (requestResponseMetadataMiddleware) MiddlewareName() string {
	return "request-response-metadata"
}

func (requestResponseMetadataMiddleware) OnModelCall(
	ctx context.Context,
	_ agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ModelCallHandler,
) (<-chan modelpkg.ChatResponse, error) {
	request := input["request"].(modelpkg.CallRequest)
	request.Metadata = cloneAnyMap(request.Metadata)
	request.Metadata["middleware_request"] = "preserved"
	input["request"] = request
	responses, err := next(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(wrapped)
		for response := range responses {
			clone := response.Clone()
			clone.Metadata = cloneAnyMap(clone.Metadata)
			clone.Metadata["middleware_response"] = "observed"
			wrapped <- *clone
		}
	}()
	return wrapped, nil
}

type recordingTracer struct {
	mu    sync.Mutex
	spans []*recordingSpan
}

func (t *recordingTracer) StartSpan(ctx context.Context, name string, attributes map[string]any) (context.Context, middleware.TraceSpan) {
	span := &recordingSpan{name: name, attributes: cloneAnyMap(attributes)}
	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()
	return ctx, span
}

func (t *recordingTracer) SpanNames() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	names := make([]string, 0, len(t.spans))
	for _, span := range t.spans {
		names = append(names, span.name)
	}
	return names
}

type recordingSpan struct {
	name       string
	attributes map[string]any
	err        error
	ended      bool
}

func (s *recordingSpan) SetAttributes(attributes map[string]any) {
	for key, value := range attributes {
		s.attributes[key] = value
	}
}

func (s *recordingSpan) RecordError(err error) {
	s.err = err
}

func (s *recordingSpan) End() {
	s.ended = true
}

func assertSpanRecorded(t *testing.T, names []string, want string) {
	t.Helper()
	for _, name := range names {
		if name == want {
			return
		}
	}
	t.Fatalf("missing span %q in %v", want, names)
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}
