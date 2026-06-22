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

package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	goclient "github.com/mark3labs/mcp-go/client"
	gomcp "github.com/mark3labs/mcp-go/mcp"

	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

// ClientOption configures an MCP client.
type ClientOption func(*clientOptions)

type clientOptions struct {
	stateful               bool
	enabledTools           []string
	enabledSet             bool
	disabledTools          []string
	executionTimeout       time.Duration
	clientInfo             gomcp.Implementation
	toolListChangedHandler ToolListChangedHandler
	continuousListening    bool
	oauthConfig            *OAuthConfig
	taskTTL                *time.Duration
}

// ToolListChangedEvent describes a server notification that invalidated the
// cached MCP tool list.
type ToolListChangedEvent struct {
	ClientName   string
	Notification gomcp.JSONRPCNotification
}

// ToolListChangedHandler is called after a tools/list_changed notification is
// observed and the local raw tool cache has been cleared.
type ToolListChangedHandler func(ToolListChangedEvent)

// WithStateful controls whether the MCP connection is persistent.
func WithStateful(stateful bool) ClientOption {
	return func(options *clientOptions) {
		options.stateful = stateful
	}
}

// WithEnabledTools limits visible tools to the provided raw MCP tool names.
func WithEnabledTools(names ...string) ClientOption {
	return func(options *clientOptions) {
		options.enabledTools = append([]string(nil), names...)
		options.enabledSet = true
	}
}

// WithDisabledTools hides the provided raw MCP tool names.
func WithDisabledTools(names ...string) ClientOption {
	return func(options *clientOptions) {
		options.disabledTools = append([]string(nil), names...)
	}
}

// WithExecutionTimeout limits MCP tool calls made through wrapped tools.
func WithExecutionTimeout(timeout time.Duration) ClientOption {
	return func(options *clientOptions) {
		options.executionTimeout = timeout
	}
}

// WithClientInfo sets the MCP initialize clientInfo payload.
func WithClientInfo(name, version string) ClientOption {
	return func(options *clientOptions) {
		options.clientInfo = gomcp.Implementation{Name: name, Version: version}
	}
}

// WithToolListChangedHandler registers a callback for MCP tool list changed
// notifications. The client clears its cached raw tool list before invoking the
// handler.
func WithToolListChangedHandler(handler ToolListChangedHandler) ClientOption {
	return func(options *clientOptions) {
		options.toolListChangedHandler = handler
	}
}

// Client manages an MCP connection and exposes MCP tools as AgentScope tools.
type Client struct {
	name             string
	stateful         bool
	enabledTools     map[string]struct{}
	enabledSet       bool
	disabledTools    map[string]struct{}
	executionTimeout time.Duration
	clientInfo       gomcp.Implementation
	toolListChanged  ToolListChangedHandler
	taskTTL          *time.Duration
	factory          clientFactory
	config           asworkspace.MCPClientConfig

	mu          sync.Mutex
	client      *goclient.Client
	connected   bool
	cachedTools []gomcp.Tool
}

// Name returns the MCP client name.
func (c *Client) Name() string {
	if c == nil {
		return ""
	}
	return c.name
}

// MCPClientConfig returns the stable JSON config used for workspace persistence.
func (c *Client) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	if c == nil {
		return asworkspace.MCPClientConfig{}, fmt.Errorf("mcp: nil client")
	}
	if c.config.Type == "" {
		return asworkspace.MCPClientConfig{}, fmt.Errorf("mcp: client %q cannot be serialized", c.name)
	}
	config := c.config
	config.EnabledTools = append([]string(nil), config.EnabledTools...)
	config.DisabledTools = append([]string(nil), config.DisabledTools...)
	if config.Stdio != nil {
		stdio := *config.Stdio
		stdio.Args = append([]string(nil), config.Stdio.Args...)
		stdio.Env = cloneStringMap(config.Stdio.Env)
		config.Stdio = &stdio
	}
	if config.HTTP != nil {
		http := *config.HTTP
		http.Headers = cloneStringMap(config.HTTP.Headers)
		config.HTTP = &http
	}
	return config, nil
}

// IsStateful reports whether this client uses a persistent connection.
func (c *Client) IsStateful() bool {
	return c != nil && c.stateful
}

// IsConnected reports whether the persistent MCP connection is active.
func (c *Client) IsConnected() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Connect starts and initializes a stateful MCP connection.
func (c *Client) Connect(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("mcp: nil client")
	}
	if !c.stateful {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connected {
		return fmt.Errorf("mcp: client %q is already connected", c.name)
	}
	client, err := c.factory(ctx)
	if err != nil {
		return err
	}
	if err := c.startAndInitialize(ctx, client); err != nil {
		_ = client.Close()
		return err
	}
	c.client = client
	c.connected = true
	c.cachedTools = nil
	return nil
}

// Close closes a stateful MCP connection.
func (c *Client) Close() error {
	if c == nil || !c.stateful {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected {
		return fmt.Errorf("mcp: client %q is not connected", c.name)
	}
	err := c.client.Close()
	c.client = nil
	c.connected = false
	c.cachedTools = nil
	return err
}

// ListRawTools lists raw MCP tools after applying enable/disable filters.
func (c *Client) ListRawTools(ctx context.Context) ([]gomcp.Tool, error) {
	if c == nil {
		return nil, fmt.Errorf("mcp: nil client")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	tools, err := c.listRawToolsLocked(ctx)
	if err != nil {
		return nil, err
	}
	return cloneRawTools(c.filterTools(tools)), nil
}

// ListTools wraps raw MCP tools as AgentScope tools.
func (c *Client) ListTools(ctx context.Context) ([]astool.Tool, error) {
	rawTools, err := c.ListRawTools(ctx)
	if err != nil {
		return nil, err
	}
	tools := make([]astool.Tool, 0, len(rawTools))
	for _, rawTool := range rawTools {
		wrapped, err := NewTool(c, rawTool)
		if err != nil {
			return nil, err
		}
		tools = append(tools, wrapped)
	}
	return tools, nil
}

// GetTool returns one wrapped MCP tool by raw MCP tool name.
func (c *Client) GetTool(ctx context.Context, rawName string) (astool.Tool, error) {
	if c == nil {
		return nil, fmt.Errorf("mcp: nil client")
	}
	c.mu.Lock()
	if c.cachedTools == nil {
		if _, err := c.listRawToolsLocked(ctx); err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}
	cached := cloneRawTools(c.cachedTools)
	c.mu.Unlock()

	for _, rawTool := range cached {
		if rawTool.Name == rawName {
			return NewTool(c, rawTool)
		}
	}
	return nil, fmt.Errorf("mcp: tool %q not found in client %q", rawName, c.name)
}

// CallTool invokes one raw MCP tool.
func (c *Client) CallTool(ctx context.Context, rawName string, input map[string]any) (*gomcp.CallToolResult, error) {
	if c == nil {
		return nil, fmt.Errorf("mcp: nil client")
	}
	if c.executionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.executionTimeout)
		defer cancel()
	}
	request := gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Name:      rawName,
			Arguments: input,
			Task:      c.taskParams(),
		},
	}
	if !c.stateful {
		var result *gomcp.CallToolResult
		err := c.withEphemeralClient(ctx, func(client *goclient.Client) error {
			var callErr error
			result, callErr = client.CallTool(ctx, request)
			return callErr
		})
		return result, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.client == nil {
		return nil, fmt.Errorf("mcp: client %q is not connected", c.name)
	}
	return c.client.CallTool(ctx, request)
}

func defaultClientOptions() clientOptions {
	return clientOptions{
		stateful: true,
		clientInfo: gomcp.Implementation{
			Name:    "AgentScope Go",
			Version: "0.0.0",
		},
	}
}

func newClient(name string, options clientOptions, config asworkspace.MCPClientConfig, factory clientFactory) (*Client, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("mcp: client name is required")
	}
	if !isProviderToolNamePart(name) {
		return nil, fmt.Errorf("mcp: client name %q contains provider-invalid characters; only letters, numbers, underscore, and hyphen are allowed", name)
	}
	if factory == nil {
		return nil, fmt.Errorf("mcp: client factory is required")
	}
	enabled, err := namesSet(options.enabledTools)
	if err != nil {
		return nil, err
	}
	disabled, err := namesSet(options.disabledTools)
	if err != nil {
		return nil, err
	}
	for name := range enabled {
		if _, exists := disabled[name]; exists {
			return nil, fmt.Errorf("mcp: enabled and disabled tools overlap on %q", name)
		}
	}
	if strings.TrimSpace(options.clientInfo.Name) == "" {
		options.clientInfo.Name = "AgentScope Go"
	}
	if strings.TrimSpace(options.clientInfo.Version) == "" {
		options.clientInfo.Version = "0.0.0"
	}
	if options.taskTTL != nil && *options.taskTTL < 0 {
		return nil, fmt.Errorf("mcp: task TTL must be non-negative")
	}
	var taskTTL *time.Duration
	if options.taskTTL != nil {
		ttl := *options.taskTTL
		taskTTL = &ttl
	}
	return &Client{
		name:             name,
		stateful:         options.stateful,
		enabledTools:     enabled,
		enabledSet:       options.enabledSet,
		disabledTools:    disabled,
		executionTimeout: options.executionTimeout,
		clientInfo:       options.clientInfo,
		toolListChanged:  options.toolListChangedHandler,
		taskTTL:          taskTTL,
		factory:          factory,
		config:           config,
	}, nil
}

func (c *Client) taskParams() *gomcp.TaskParams {
	if c == nil || c.taskTTL == nil {
		return nil
	}
	task := &gomcp.TaskParams{}
	if *c.taskTTL > 0 {
		ttl := c.taskTTL.Milliseconds()
		task.TTL = &ttl
	}
	return task
}

func (c *Client) listRawToolsLocked(ctx context.Context) ([]gomcp.Tool, error) {
	var result *gomcp.ListToolsResult
	if !c.stateful {
		err := c.withEphemeralClient(ctx, func(client *goclient.Client) error {
			var listErr error
			result, listErr = client.ListTools(ctx, gomcp.ListToolsRequest{})
			return listErr
		})
		if err != nil {
			return nil, err
		}
	} else {
		if !c.connected || c.client == nil {
			return nil, fmt.Errorf("mcp: client %q is not connected", c.name)
		}
		var err error
		result, err = c.client.ListTools(ctx, gomcp.ListToolsRequest{})
		if err != nil {
			return nil, err
		}
	}
	c.cachedTools = cloneRawTools(result.Tools)
	return result.Tools, nil
}

func (c *Client) withEphemeralClient(ctx context.Context, fn func(*goclient.Client) error) error {
	client, err := c.factory(ctx)
	if err != nil {
		return err
	}
	if err := c.startAndInitialize(ctx, client); err != nil {
		_ = client.Close()
		return err
	}
	defer func() { _ = client.Close() }()
	return fn(client)
}

func (c *Client) startAndInitialize(ctx context.Context, client *goclient.Client) error {
	client.OnNotification(c.handleNotification)
	if err := client.Start(ctx); err != nil {
		return err
	}
	request := gomcp.InitializeRequest{}
	request.Params.ProtocolVersion = gomcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = c.clientInfo
	request.Params.Capabilities = gomcp.ClientCapabilities{}
	if _, err := client.Initialize(ctx, request); err != nil {
		return err
	}
	return nil
}

func (c *Client) handleNotification(notification gomcp.JSONRPCNotification) {
	if notification.Method != string(gomcp.MethodNotificationToolsListChanged) {
		return
	}
	event := ToolListChangedEvent{
		ClientName:   c.name,
		Notification: notification,
	}
	if c.mu.TryLock() {
		c.cachedTools = nil
		c.mu.Unlock()
		c.notifyToolListChanged(event)
		return
	}
	go func() {
		c.mu.Lock()
		c.cachedTools = nil
		c.mu.Unlock()
		c.notifyToolListChanged(event)
	}()
}

func (c *Client) notifyToolListChanged(event ToolListChangedEvent) {
	if c.toolListChanged != nil {
		c.toolListChanged(event)
	}
}

func (c *Client) filterTools(tools []gomcp.Tool) []gomcp.Tool {
	filtered := make([]gomcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if c.enabledSet {
			if _, ok := c.enabledTools[tool.Name]; !ok {
				continue
			}
		}
		if _, disabled := c.disabledTools[tool.Name]; disabled {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func namesSet(names []string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("mcp: tool filter name is empty")
		}
		out[name] = struct{}{}
	}
	return out, nil
}

func cloneRawTools(in []gomcp.Tool) []gomcp.Tool {
	return append([]gomcp.Tool(nil), in...)
}
