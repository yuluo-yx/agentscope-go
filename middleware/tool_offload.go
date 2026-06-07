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

package middleware

import (
	"context"
	"fmt"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

const defaultToolOffloadTimeout = 30 * time.Second

// ToolOffloadResult is delivered after a timed-out tool finishes in the background.
type ToolOffloadResult struct {
	AgentName string
	SessionID string
	ToolCall  *message.ToolCallBlock
	Chunks    []agentpkg.ToolChunk
	Err       error
}

// ToolOffloadSink receives background tool completion results.
type ToolOffloadSink interface {
	CompleteOffloadedTool(context.Context, ToolOffloadResult) error
}

// ToolOffloadOption configures ToolOffloadMiddleware.
type ToolOffloadOption func(*ToolOffloadMiddleware)

// WithToolOffloadTimeout sets the duration after which a running tool is offloaded.
func WithToolOffloadTimeout(timeout time.Duration) ToolOffloadOption {
	return func(m *ToolOffloadMiddleware) {
		m.timeout = timeout
	}
}

// ToolOffloadMiddleware lets long-running tool executions continue in the background.
type ToolOffloadMiddleware struct {
	timeout time.Duration
	sink    ToolOffloadSink
}

// NewToolOffloadMiddleware creates a tool offload middleware.
func NewToolOffloadMiddleware(sink ToolOffloadSink, opts ...ToolOffloadOption) *ToolOffloadMiddleware {
	m := &ToolOffloadMiddleware{
		timeout: defaultToolOffloadTimeout,
		sink:    sink,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// MiddlewareName returns the middleware name.
func (*ToolOffloadMiddleware) MiddlewareName() string {
	return "tool-offload"
}

// OnActing returns a running placeholder when a tool does not finish before the configured timeout.
func (m *ToolOffloadMiddleware) OnActing(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ToolHandler,
) (<-chan agentpkg.ToolChunk, error) {
	if m == nil || m.timeout <= 0 {
		return next(ctx)
	}
	toolCall, _ := input["tool_call"].(*message.ToolCallBlock)
	chunks, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if chunks == nil {
		return chunks, nil
	}
	done := make(chan offloadCollection, 1)
	go func() {
		collected, collectErr := collectToolChunks(chunks)
		done <- offloadCollection{chunks: collected, err: collectErr}
	}()

	timer := time.NewTimer(m.timeout)
	defer timer.Stop()
	select {
	case result := <-done:
		return replayToolChunks(result.chunks, result.err), nil
	case <-timer.C:
		go m.deliverOffloadedResult(context.WithoutCancel(ctx), agent, toolCall, done)
		return singleToolChunk(offloadedPlaceholder(toolCall)), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *ToolOffloadMiddleware) deliverOffloadedResult(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	toolCall *message.ToolCallBlock,
	done <-chan offloadCollection,
) {
	result := <-done
	if m.sink == nil {
		return
	}
	_ = m.sink.CompleteOffloadedTool(ctx, ToolOffloadResult{
		AgentName: agent.AgentName(),
		SessionID: sessionID(agent),
		ToolCall:  cloneToolCall(toolCall),
		Chunks:    result.chunks,
		Err:       result.err,
	})
}

type offloadCollection struct {
	chunks []agentpkg.ToolChunk
	err    error
}

func collectToolChunks(chunks <-chan agentpkg.ToolChunk) ([]agentpkg.ToolChunk, error) {
	collected := []agentpkg.ToolChunk{}
	for chunk := range chunks {
		clone := chunk.Clone()
		if clone == nil {
			continue
		}
		collected = append(collected, *clone)
	}
	return collected, nil
}

func replayToolChunks(chunks []agentpkg.ToolChunk, err error) <-chan agentpkg.ToolChunk {
	if err != nil {
		return singleToolChunk(*astool.NewToolChunk(
			message.ContentBlockList{message.NewTextBlock(err.Error())},
			astool.WithToolChunkState(message.ToolResultError),
		))
	}
	out := make(chan agentpkg.ToolChunk, len(chunks))
	for _, chunk := range chunks {
		out <- chunk
	}
	close(out)
	return out
}

func singleToolChunk(chunk agentpkg.ToolChunk) <-chan agentpkg.ToolChunk {
	out := make(chan agentpkg.ToolChunk, 1)
	out <- chunk
	close(out)
	return out
}

func offloadedPlaceholder(toolCall *message.ToolCallBlock) agentpkg.ToolChunk {
	toolName := "tool"
	toolID := ""
	if toolCall != nil {
		toolName = toolCall.Name
		toolID = toolCall.ID
	}
	return *astool.NewToolChunk(
		message.ContentBlockList{message.NewTextBlock(fmt.Sprintf("Tool %s is still running; its result will be delivered asynchronously.", toolName))},
		astool.WithToolChunkID(toolID),
		astool.WithToolChunkState(message.ToolResultRunning),
		astool.WithToolChunkMetadata(map[string]any{"agentscope.offloaded": true}),
	)
}

func cloneToolCall(toolCall *message.ToolCallBlock) *message.ToolCallBlock {
	if toolCall == nil {
		return nil
	}
	return toolCall.Clone().(*message.ToolCallBlock)
}
