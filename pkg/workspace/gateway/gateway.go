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

	gomcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace"
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
	baseURL     string
	httpClient  *http.Client
	headers     map[string]string
	bearerToken string
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

// WithHeaders sets static headers sent to every gateway request.
func WithHeaders(headers map[string]string) Option {
	return func(gateway *Client) {
		gateway.headers = cloneStringMap(headers)
	}
}

// WithBearerToken sends Authorization: Bearer on every gateway request.
func WithBearerToken(token string) Option {
	return func(gateway *Client) {
		gateway.bearerToken = strings.TrimSpace(token)
	}
}

// NewHTTPClient creates a gateway client for a base URL.
func NewHTTPClient(baseURL string, opts ...Option) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("workspace/gateway: base URL is empty")
	}
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("workspace/gateway: unsupported base URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("workspace/gateway: base URL host is empty")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("workspace/gateway: base URL must not include user info, query, or fragment")
	}
	client := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		headers:    map[string]string{},
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
	return c.do(ctx, http.MethodGet, "/health", nil, nil, http.StatusOK, http.StatusNoContent)
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

// NewMCPClient creates a host-side proxy for one gateway-registered MCP.
func (c *Client) NewMCPClient(config workspace.MCPClientConfig, connected bool) workspace.MCPClient {
	return &MCPClient{client: c, config: cloneMCPClientConfig(config), connected: connected}
}

// ListMCPTools returns wrapped tools for one gateway-registered MCP.
func (c *Client) ListMCPTools(ctx context.Context, name string) ([]workspace.Tool, error) {
	client := &MCPClient{client: c, config: workspace.MCPClientConfig{Name: name}, connected: true}
	return client.ListTools(ctx)
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

func (c *Client) listMCPRawTools(ctx context.Context, name string) ([]gomcp.Tool, error) {
	var tools []gomcp.Tool
	path := "/mcps/" + url.PathEscape(name) + "/tools"
	if err := c.do(ctx, http.MethodGet, path, nil, &tools, http.StatusOK); err != nil {
		return nil, err
	}
	return tools, nil
}

func (c *Client) callMCPTool(ctx context.Context, mcpName, toolName string, input map[string]any) (*tool.ToolChunk, error) {
	var response struct {
		Chunk *tool.ToolChunk `json:"chunk"`
	}
	path := "/mcps/" + url.PathEscape(mcpName) + "/tools/" + url.PathEscape(toolName)
	if err := c.do(ctx, http.MethodPost, path, map[string]any{"arguments": input}, &response, http.StatusOK); err != nil {
		return nil, err
	}
	if response.Chunk == nil {
		return nil, fmt.Errorf("workspace/gateway: gateway returned no chunk for MCP tool %q", toolName)
	}
	if response.Chunk.State == "" || response.Chunk.State == message.ToolResultRunning {
		response.Chunk.State = message.ToolResultSuccess
	}
	return response.Chunk, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any, okStatuses ...int) error {
	if c == nil {
		return fmt.Errorf("workspace/gateway: nil client")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return fmt.Errorf("workspace/gateway: invalid internal path %q", path)
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
	// #nosec G704 -- baseURL is validated by NewHTTPClient and path is an internal route assembled by this package.
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range c.headers {
		if strings.TrimSpace(key) != "" {
			request.Header.Set(key, value)
		}
	}
	if c.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}
	// #nosec G704 -- request was constructed from the validated gateway base URL and an internal route above.
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
	return t.descriptor.MCPName != ""
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

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

var _ tool.Tool = (*gatewayTool)(nil)

// MCPClient is a gateway-backed MCP proxy.
type MCPClient struct {
	client      *Client
	config      workspace.MCPClientConfig
	connected   bool
	cachedTools []gomcp.Tool
}

// Name returns the registered MCP name.
func (c *MCPClient) Name() string {
	if c == nil {
		return ""
	}
	return c.config.Name
}

// IsStateful reports whether the upstream MCP config is stateful.
func (c *MCPClient) IsStateful() bool {
	return c != nil && c.config.Stateful
}

// IsConnected reports whether this proxy is registered with the gateway.
func (c *MCPClient) IsConnected() bool {
	return c != nil && c.connected
}

// Connect registers the upstream MCP config on the gateway.
func (c *MCPClient) Connect(ctx context.Context) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workspace/gateway: nil MCP client")
	}
	if !c.config.Stateful {
		return nil
	}
	if c.connected {
		return fmt.Errorf("workspace/gateway: MCP %q is already connected", c.Name())
	}
	if err := c.client.AddMCP(ctx, c.config); err != nil {
		return err
	}
	c.connected = true
	c.cachedTools = nil
	return nil
}

// Close unregisters the upstream MCP config from the gateway.
func (c *MCPClient) Close() error {
	if c == nil || c.client == nil || !c.config.Stateful || !c.connected {
		return nil
	}
	if err := c.client.RemoveMCP(context.Background(), c.Name()); err != nil {
		return err
	}
	c.connected = false
	c.cachedTools = nil
	return nil
}

// MCPClientConfig returns the persisted upstream MCP config.
func (c *MCPClient) MCPClientConfig() (workspace.MCPClientConfig, error) {
	if c == nil {
		return workspace.MCPClientConfig{}, fmt.Errorf("workspace/gateway: nil MCP client")
	}
	return cloneMCPClientConfig(c.config), nil
}

// ListTools returns gateway-wrapped MCP tools.
func (c *MCPClient) ListTools(ctx context.Context) ([]workspace.Tool, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workspace/gateway: nil MCP client")
	}
	if c.config.Stateful && !c.connected {
		return nil, fmt.Errorf("workspace/gateway: MCP %q is not connected", c.Name())
	}
	rawTools, err := c.listRawTools(ctx)
	if err != nil {
		return nil, err
	}
	tools := make([]workspace.Tool, 0, len(rawTools))
	for _, raw := range rawTools {
		tools = append(tools, &gatewayMCPTool{
			client:      c.client,
			mcpName:     c.Name(),
			raw:         raw,
			name:        qualifiedMCPToolName(c.Name(), raw.Name),
			inputSchema: rawInputSchemaMap(raw),
			readOnly:    rawReadOnly(raw),
		})
	}
	return tools, nil
}

func (c *MCPClient) listRawTools(ctx context.Context) ([]gomcp.Tool, error) {
	rawTools, err := c.client.listMCPRawTools(ctx, c.Name())
	if err != nil {
		return nil, err
	}
	c.cachedTools = append([]gomcp.Tool(nil), rawTools...)
	return filterRawTools(rawTools, c.config.EnabledTools, c.config.DisabledTools)
}

type gatewayMCPTool struct {
	client      *Client
	mcpName     string
	raw         gomcp.Tool
	name        string
	inputSchema map[string]any
	readOnly    bool
}

func (t *gatewayMCPTool) Name() string {
	return t.name
}

func (t *gatewayMCPTool) Description() string {
	return t.raw.Description
}

func (t *gatewayMCPTool) InputSchema() map[string]any {
	return cloneAnyMap(t.inputSchema)
}

func (t *gatewayMCPTool) IsConcurrencySafe() bool {
	return false
}

func (t *gatewayMCPTool) IsReadOnly() bool {
	return t.readOnly
}

func (t *gatewayMCPTool) IsExternalTool() bool {
	return false
}

func (t *gatewayMCPTool) IsStateInjected() bool {
	return false
}

func (t *gatewayMCPTool) IsMCP() bool {
	return true
}

func (t *gatewayMCPTool) MCPName() string {
	return t.mcpName
}

func (t *gatewayMCPTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	if t.readOnly {
		return &permission.Decision{
			Behavior:       permission.BehaviorAllow,
			Message:        "This is a read-only MCP tool. Allowing execution.",
			DecisionReason: "MCP readOnlyHint is true",
		}, nil
	}
	return &permission.Decision{
		Behavior:       permission.BehaviorAsk,
		Message:        "MCP tools must be explicitly allowed by the user.",
		DecisionReason: "MCP tool is not read-only",
	}, nil
}

func (t *gatewayMCPTool) MatchRule(ruleContent string, _ map[string]any) bool {
	ruleContent = strings.TrimSpace(ruleContent)
	if ruleContent == "" {
		return true
	}
	return ruleContent == t.name ||
		ruleContent == t.raw.Name ||
		ruleContent == t.mcpName+"."+t.raw.Name ||
		ruleContent == t.mcpName+":"+t.raw.Name
}

func (t *gatewayMCPTool) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName:    t.name,
		RuleContent: t.mcpName + "." + t.raw.Name,
		Behavior:    permission.BehaviorAllow,
		Source:      "suggested",
	}}
}

func (t *gatewayMCPTool) Execute(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	chunk, err := t.client.callMCPTool(ctx, t.mcpName, t.raw.Name, input)
	if err != nil {
		return singleChunk(tool.NewToolChunk(message.ContentBlockList{message.NewTextBlock(err.Error())}, tool.WithToolChunkState(message.ToolResultError))), nil
	}
	return singleChunk(chunk), nil
}

func qualifiedMCPToolName(mcpName, toolName string) string {
	return "mcp__" + mcpName + "__" + toolName
}

func rawReadOnly(raw gomcp.Tool) bool {
	return raw.Annotations.ReadOnlyHint != nil && *raw.Annotations.ReadOnlyHint
}

func rawInputSchemaMap(raw gomcp.Tool) map[string]any {
	var data []byte
	var err error
	if raw.RawInputSchema != nil {
		data = raw.RawInputSchema
	} else {
		data, err = json.Marshal(raw.InputSchema)
		if err != nil {
			data = nil
		}
	}
	var schema map[string]any
	if len(data) > 0 {
		_ = json.Unmarshal(data, &schema)
	}
	if schema == nil {
		schema = map[string]any{}
	}
	if schemaType, _ := schema["type"].(string); schemaType == "" {
		schema["type"] = "object"
	}
	if _, ok := schema["properties"]; !ok || schema["properties"] == nil {
		schema["properties"] = map[string]any{}
	}
	if _, ok := schema["required"]; !ok || schema["required"] == nil {
		schema["required"] = []any{}
	}
	return schema
}

func filterRawTools(rawTools []gomcp.Tool, enabledTools, disabledTools []string) ([]gomcp.Tool, error) {
	enabled, enabledSet, err := toolNameSet(enabledTools)
	if err != nil {
		return nil, err
	}
	disabled, _, err := toolNameSet(disabledTools)
	if err != nil {
		return nil, err
	}
	for name := range enabled {
		if _, exists := disabled[name]; exists {
			return nil, fmt.Errorf("workspace/gateway: enabled and disabled tools overlap on %q", name)
		}
	}
	out := make([]gomcp.Tool, 0, len(rawTools))
	for _, raw := range rawTools {
		if enabledSet {
			if _, ok := enabled[raw.Name]; !ok {
				continue
			}
		}
		if _, ok := disabled[raw.Name]; ok {
			continue
		}
		out = append(out, raw)
	}
	return out, nil
}

func toolNameSet(names []string) (map[string]struct{}, bool, error) {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, false, fmt.Errorf("workspace/gateway: tool filter name is empty")
		}
		out[name] = struct{}{}
	}
	return out, len(names) > 0, nil
}

func cloneMCPClientConfig(config workspace.MCPClientConfig) workspace.MCPClientConfig {
	config.EnabledTools = append([]string(nil), config.EnabledTools...)
	config.DisabledTools = append([]string(nil), config.DisabledTools...)
	if config.Stdio != nil {
		stdio := *config.Stdio
		stdio.Args = append([]string(nil), config.Stdio.Args...)
		stdio.Env = cloneStringMap(config.Stdio.Env)
		config.Stdio = &stdio
	}
	if config.HTTP != nil {
		httpConfig := *config.HTTP
		httpConfig.Headers = cloneStringMap(config.HTTP.Headers)
		config.HTTP = &httpConfig
	}
	return config
}

var (
	_ workspace.MCPClient = (*MCPClient)(nil)
	_ tool.Tool           = (*gatewayMCPTool)(nil)
)
