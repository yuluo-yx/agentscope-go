package testcases

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
)

type scriptedChatModel struct {
	name      string
	responses []*modelpkg.ChatResponse
	requests  []modelpkg.CallRequest
}

func (m *scriptedChatModel) Name() string {
	if m.name != "" {
		return m.name
	}
	return "scripted-e2e"
}

func (m *scriptedChatModel) Call(_ context.Context, request modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model %s has no response", m.Name())
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func (m *scriptedChatModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model %s has no response", m.Name())
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	ch := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(ch)
		delta := response.Clone()
		delta.IsLast = false
		delta.Usage = nil
		select {
		case ch <- *delta:
		case <-ctx.Done():
			return
		}
		select {
		case ch <- *response.Clone():
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (m *scriptedChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

type asyncErrorChatModel struct {
	err error
}

func (m asyncErrorChatModel) Name() string { return "async-error-e2e" }

func (m asyncErrorChatModel) Call(context.Context, modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	return nil, m.err
}

func (m asyncErrorChatModel) Stream(context.Context, modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	out := make(chan modelpkg.ChatResponse, 1)
	out <- *modelpkg.NewChatResponse(nil, true, modelpkg.WithChatResponseError(m.err))
	close(out)
	return out, nil
}

func (m asyncErrorChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func jsonInput(value map[string]any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func mustJSONInput(value map[string]any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func toolResultsFromLastRequest(model *scriptedChatModel) ([]*message.ToolResultBlock, error) {
	if len(model.requests) == 0 {
		return nil, fmt.Errorf("model has no recorded requests")
	}
	request := model.requests[len(model.requests)-1]
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("last request has no messages")
	}
	last := request.Messages[len(request.Messages)-1]
	blocks := last.GetContentBlocks("tool_result")
	results := make([]*message.ToolResultBlock, 0, len(blocks))
	for _, block := range blocks {
		result, ok := block.(*message.ToolResultBlock)
		if !ok {
			return nil, fmt.Errorf("tool_result block has unexpected type %T", block)
		}
		results = append(results, result)
	}
	return results, nil
}

func onlyToolResultFromLastRequest(model *scriptedChatModel) (*message.ToolResultBlock, error) {
	results, err := toolResultsFromLastRequest(model)
	if err != nil {
		return nil, err
	}
	if len(results) != 1 {
		return nil, fmt.Errorf("expected one tool result, got %d", len(results))
	}
	return results[0], nil
}

func lastToolResultFromLastRequest(model *scriptedChatModel) (*message.ToolResultBlock, error) {
	results, err := toolResultsFromLastRequest(model)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("expected at least one tool result")
	}
	return results[len(results)-1], nil
}

func requestIncludesTool(request modelpkg.CallRequest, name string) bool {
	for _, schema := range request.Tools {
		if schema.Function.Name == name {
			return true
		}
	}
	return false
}

func assertText(blocks message.ContentBlockList, want string) error {
	text := blocks.GetTextContent("")
	if text == nil || *text != want {
		return fmt.Errorf("text mismatch: got %#v want %q", text, want)
	}
	return nil
}

func assertEventOrder(events []message.Event, expected ...message.EventType) error {
	next := 0
	for _, event := range events {
		if next < len(expected) && event.GetType() == expected[next] {
			next++
		}
	}
	if next == len(expected) {
		return nil
	}
	types := make([]message.EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.GetType())
	}
	return fmt.Errorf("event order mismatch: expected subsequence %v in %v", expected, types)
}

func findToolByName(tools []tool.Tool, name string) (tool.Tool, error) {
	for _, current := range tools {
		if current.Name() == name {
			return current, nil
		}
	}
	return nil, fmt.Errorf("missing tool %q", name)
}

func runRepoGoTest(ctx context.Context, pattern string, args ...string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	goArgs := append([]string{"test", pattern}, args...)
	cmd := exec.CommandContext(ctx, "go", goArgs...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "DASHSCOPE_API_KEY=", "AI_DASHSCOPE_API_KEY=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go %s failed: %w\n%s", strings.Join(goArgs, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if data, readErr := os.ReadFile(filepath.Join(wd, "go.mod")); readErr == nil &&
			strings.Contains(string(data), "module github.com/yuluo-yx/agentscope-go") &&
			fileExists(filepath.Join(wd, "pkg", "agent", "agent.go")) &&
			fileExists(filepath.Join(wd, "facade.go")) {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "", fmt.Errorf("could not locate agentscope-go repository root")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func shortTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 && timeout < 30*time.Second {
		return timeout
	}
	return 30 * time.Second
}
