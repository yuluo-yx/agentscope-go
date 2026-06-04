package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/workspace"
)

// ToolDescriptor describes one tool exposed by the in-workspace MCP gateway.
type ToolDescriptor struct {
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	InputSchema     map[string]any `json:"input_schema"`
	MCPName         string         `json:"mcp_name"`
	ReadOnly        bool           `json:"read_only"`
	ConcurrencySafe bool           `json:"concurrency_safe"`
}

// ToolCallResponse is the response from one gateway tool call.
type ToolCallResponse struct {
	State  message.ToolResultState  `json:"state"`
	Blocks message.ContentBlockList `json:"blocks"`
}

// Client is the host-side client for the workspace MCP gateway.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures a gateway client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client used for gateway requests.
func WithHTTPClient(client *http.Client) Option {
	return func(gateway *Client) {
		if client != nil {
			gateway.httpClient = client
		}
	}
}

// NewHTTPClient creates a gateway client for a base URL.
func NewHTTPClient(baseURL string, opts ...Option) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("workspace/gateway: base URL is empty")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, err
	}
	client := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client, nil
}

// Bootstrap checks whether the gateway is ready.
func (c *Client) Bootstrap(ctx context.Context) error {
	return c.Health(ctx)
}

// Health probes the gateway health endpoint.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/health", nil, nil, http.StatusOK)
}

// AddMCP registers one MCP server in the gateway.
func (c *Client) AddMCP(ctx context.Context, config workspace.MCPClientConfig) error {
	return c.do(ctx, http.MethodPost, "/mcps", config, nil, http.StatusOK, http.StatusCreated, http.StatusNoContent)
}

// RemoveMCP unregisters one MCP server from the gateway.
func (c *Client) RemoveMCP(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/mcps/"+url.PathEscape(name), nil, nil, http.StatusOK, http.StatusNoContent, http.StatusNotFound)
}

// ListMCPs returns MCP configs currently registered in the gateway.
func (c *Client) ListMCPs(ctx context.Context) ([]workspace.MCPClientConfig, error) {
	var configs []workspace.MCPClientConfig
	if err := c.do(ctx, http.MethodGet, "/mcps", nil, &configs, http.StatusOK); err != nil {
		return nil, err
	}
	return configs, nil
}

// ListTools returns MCP tools exposed by the gateway.
func (c *Client) ListTools(ctx context.Context) ([]workspace.Tool, error) {
	var descriptors []ToolDescriptor
	if err := c.do(ctx, http.MethodGet, "/tools", nil, &descriptors, http.StatusOK); err != nil {
		return nil, err
	}
	tools := make([]workspace.Tool, 0, len(descriptors))
	for _, descriptor := range descriptors {
		tools = append(tools, &gatewayTool{client: c, descriptor: descriptor})
	}
	return tools, nil
}

// Close asks the gateway to release registered MCP clients and runtime state.
func (c *Client) Close(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/close", nil, nil, http.StatusOK, http.StatusNoContent, http.StatusNotFound)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any, okStatuses ...int) error {
	if c == nil {
		return fmt.Errorf("workspace/gateway: nil client")
	}
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if !statusAllowed(response.StatusCode, okStatuses) {
		return fmt.Errorf("workspace/gateway: %s %s returned HTTP %d", method, path, response.StatusCode)
	}
	if out == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(out)
}

func statusAllowed(status int, allowed []int) bool {
	for _, current := range allowed {
		if status == current {
			return true
		}
	}
	return false
}

type gatewayTool struct {
	client     *Client
	descriptor ToolDescriptor
}

func (t *gatewayTool) Name() string {
	return t.descriptor.Name
}

func (t *gatewayTool) Description() string {
	return t.descriptor.Description
}

func (t *gatewayTool) InputSchema() map[string]any {
	if t.descriptor.InputSchema == nil {
		return map[string]any{"type": "object"}
	}
	return cloneAnyMap(t.descriptor.InputSchema)
}

func (t *gatewayTool) IsConcurrencySafe() bool {
	return t.descriptor.ConcurrencySafe
}

func (t *gatewayTool) IsReadOnly() bool {
	return t.descriptor.ReadOnly
}

func (t *gatewayTool) IsExternalTool() bool {
	return false
}

func (t *gatewayTool) IsStateInjected() bool {
	return false
}

func (t *gatewayTool) IsMCP() bool {
	return true
}

func (t *gatewayTool) MCPName() string {
	return t.descriptor.MCPName
}

func (t *gatewayTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	if t.IsReadOnly() {
		return &permission.Decision{
			Behavior:       permission.BehaviorAllow,
			Message:        "Gateway MCP tool is read-only.",
			DecisionReason: "MCP readOnlyHint is true",
		}, nil
	}
	return &permission.Decision{
		Behavior:       permission.BehaviorAsk,
		Message:        "Gateway MCP tools must be explicitly allowed.",
		DecisionReason: "MCP tool is not read-only",
		SuggestedRules: t.GenerateSuggestions(nil),
	}, nil
}

func (t *gatewayTool) MatchRule(ruleContent string, _ map[string]any) bool {
	return ruleContent == "" || ruleContent == t.MCPName()
}

func (t *gatewayTool) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName:    t.Name(),
		RuleContent: t.MCPName(),
		Behavior:    permission.BehaviorAllow,
		Source:      "suggested",
	}}
}

func (t *gatewayTool) Execute(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	var response ToolCallResponse
	path := "/tools/" + url.PathEscape(t.Name()) + "/call"
	if err := t.client.do(ctx, http.MethodPost, path, input, &response, http.StatusOK); err != nil {
		return singleChunk(tool.NewToolChunk(message.ContentBlockList{message.NewTextBlock(err.Error())}, tool.WithToolChunkState(message.ToolResultError))), nil
	}
	state := response.State
	if state == "" {
		state = message.ToolResultSuccess
	}
	return singleChunk(tool.NewToolChunk(response.Blocks, tool.WithToolChunkState(state))), nil
}

func singleChunk(chunk *tool.ToolChunk) <-chan tool.ToolChunk {
	chunks := make(chan tool.ToolChunk, 1)
	if chunk != nil {
		chunks <- *chunk
	}
	close(chunks)
	return chunks
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

var _ tool.Tool = (*gatewayTool)(nil)
