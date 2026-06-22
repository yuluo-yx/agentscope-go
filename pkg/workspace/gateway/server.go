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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
	toolmcp "github.com/yuluo-yx/agentscope-go/pkg/tool/mcp"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

const defaultMaxRequestBytes int64 = 32 << 20

// ServerOption configures an in-workspace gateway HTTP server.
type ServerOption func(*Server)

// Server serves workspace tools and gateway-managed MCP clients over HTTP.
type Server struct {
	mu         sync.Mutex
	tools      map[string]workspace.Tool
	mcps       map[string]workspace.MCPClient
	mcpFactory workspace.MCPClientFactory
	token      string
	maxBytes   int64
}

// NewServer creates a gateway HTTP handler. The handler is intentionally small
// enough to be embedded in Docker, Agent Sandbox, or local test runtimes.
func NewServer(opts ...ServerOption) *Server {
	server := &Server{
		tools:      map[string]workspace.Tool{},
		mcps:       map[string]workspace.MCPClient{},
		mcpFactory: defaultServerMCPFactory,
		maxBytes:   defaultMaxRequestBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}
	if server.mcpFactory == nil {
		server.mcpFactory = defaultServerMCPFactory
	}
	return server
}

// WithServerTools exposes non-MCP tools through /tools.
func WithServerTools(tools ...workspace.Tool) ServerOption {
	return func(server *Server) {
		for _, current := range tools {
			if current == nil || strings.TrimSpace(current.Name()) == "" {
				continue
			}
			server.tools[current.Name()] = current
		}
	}
}

// WithServerMCPs pre-registers MCP clients before the server starts.
func WithServerMCPs(mcps ...workspace.MCPClient) ServerOption {
	return func(server *Server) {
		for _, current := range mcps {
			if current == nil || strings.TrimSpace(current.Name()) == "" {
				continue
			}
			server.mcps[current.Name()] = current
		}
	}
}

// WithServerMCPClientFactory sets the factory used by POST /mcps.
func WithServerMCPClientFactory(factory workspace.MCPClientFactory) ServerOption {
	return func(server *Server) {
		server.mcpFactory = factory
	}
}

// WithServerBearerToken protects every endpoint except /health.
func WithServerBearerToken(token string) ServerOption {
	return func(server *Server) {
		server.token = strings.TrimSpace(token)
	}
}

// WithServerMaxRequestBytes limits JSON request bodies accepted by the server.
func WithServerMaxRequestBytes(maxBytes int64) ServerOption {
	return func(server *Server) {
		if maxBytes > 0 {
			server.maxBytes = maxBytes
		}
	}
}

// ServeHTTP routes gateway HTTP requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		writeError(w, http.StatusInternalServerError, "workspace/gateway: nil server")
		return
	}
	path := normalizedPath(r.URL.Path)
	if r.Method == http.MethodGet && path == "/health" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.route(w, r, path)
}

func normalizedPath(path string) string {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

func (s *Server) route(w http.ResponseWriter, r *http.Request, path string) {
	if s.routeExact(w, r, path) {
		return
	}
	if s.routeToolCall(w, r, path) {
		return
	}
	if s.routeMCPByName(w, r, path) {
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) routeExact(w http.ResponseWriter, r *http.Request, path string) bool {
	switch {
	case r.Method == http.MethodGet && path == "/tools":
		s.handleListTools(w, r)
	case r.Method == http.MethodGet && path == "/mcps":
		s.handleListMCPs(w, r)
	case r.Method == http.MethodPost && path == "/mcps":
		s.handleAddMCP(w, r)
	case r.Method == http.MethodPost && path == "/close":
		s.handleClose(w, r)
	default:
		return false
	}
	return true
}

func (s *Server) routeToolCall(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method != http.MethodPost || !strings.HasPrefix(path, "/tools/") || !strings.HasSuffix(path, "/call") {
		return false
	}
	s.handleToolCall(w, r, path)
	return true
}

func (s *Server) routeMCPByName(w http.ResponseWriter, r *http.Request, path string) bool {
	if !strings.HasPrefix(path, "/mcps/") {
		return false
	}
	switch {
	case r.Method == http.MethodDelete && !strings.Contains(strings.TrimPrefix(path, "/mcps/"), "/"):
		s.handleRemoveMCP(w, r, path)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/tools"):
		s.handleListMCPTools(w, r, path)
	case r.Method == http.MethodPost && strings.Contains(path, "/tools/"):
		s.handleMCPToolCall(w, r, path)
	default:
		return false
	}
	return true
}

func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return false
	}
	return r.Header.Get("Authorization") == "Bearer "+s.token
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools, err := s.allTools(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	descriptors := make([]ToolDescriptor, 0, len(tools))
	for _, current := range tools {
		if current == nil {
			continue
		}
		descriptors = append(descriptors, descriptorFromTool(current))
	}
	writeJSON(w, http.StatusOK, descriptors)
}

func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request, path string) {
	escaped := strings.TrimSuffix(strings.TrimPrefix(path, "/tools/"), "/call")
	name, err := url.PathUnescape(escaped)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var input map[string]any
	err = s.decodeJSON(w, r, &input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := s.findTool(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	response, err := runGatewayTool(r.Context(), current, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListMCPs(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	configs := make([]workspace.MCPClientConfig, 0, len(s.mcps))
	for _, client := range s.mcps {
		config, err := configFromMCP(client)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		configs = append(configs, config)
	}
	writeJSON(w, http.StatusOK, configs)
}

func (s *Server) handleAddMCP(w http.ResponseWriter, r *http.Request) {
	var config workspace.MCPClientConfig
	if err := s.decodeJSON(w, r, &config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(config.Name) == "" {
		writeError(w, http.StatusBadRequest, "workspace/gateway: MCP name is required")
		return
	}
	if s.getMCP(config.Name) != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("workspace/gateway: MCP %q already exists", config.Name))
		return
	}
	client, err := s.mcpFactory(config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if client == nil {
		writeError(w, http.StatusInternalServerError, "workspace/gateway: MCP factory returned nil")
		return
	}
	if err := ensureMCPConnected(r.Context(), client); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.mcps[client.Name()]; exists {
		if client.IsStateful() && client.IsConnected() {
			_ = client.Close()
		}
		writeError(w, http.StatusConflict, fmt.Sprintf("workspace/gateway: MCP %q already exists", client.Name()))
		return
	}
	s.mcps[client.Name()] = client
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveMCP(w http.ResponseWriter, r *http.Request, path string) {
	name, err := url.PathUnescape(strings.TrimPrefix(path, "/mcps/"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client := s.popMCP(name)
	if client == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("workspace/gateway: MCP %q not found", name))
		return
	}
	if client.IsStateful() && client.IsConnected() {
		if err := client.Close(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	_ = r.Body.Close()
}

func (s *Server) handleListMCPTools(w http.ResponseWriter, r *http.Request, path string) {
	name, err := mcpNameFromToolsPath(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client := s.getMCP(name)
	if client == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("workspace/gateway: MCP %q not found", name))
		return
	}
	rawTools, err := listRawTools(r.Context(), client)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rawTools)
}

func (s *Server) handleMCPToolCall(w http.ResponseWriter, r *http.Request, path string) {
	mcpName, rawToolName, err := mcpAndToolNameFromCallPath(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client := s.getMCP(mcpName)
	if client == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("workspace/gateway: MCP %q not found", mcpName))
		return
	}
	var request struct {
		Arguments map[string]any `json:"arguments"`
	}
	err = s.decodeJSON(w, r, &request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	chunk, err := callRawMCPTool(r.Context(), client, rawToolName, request.Arguments)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]*astool.ToolChunk{"chunk": chunk})
}

func (s *Server) handleClose(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	clients := make([]workspace.MCPClient, 0, len(s.mcps))
	for _, client := range s.mcps {
		clients = append(clients, client)
	}
	s.mcps = map[string]workspace.MCPClient{}
	s.mu.Unlock()
	for _, client := range clients {
		if client != nil && client.IsStateful() && client.IsConnected() {
			if err := client.Close(); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) allTools(ctx context.Context) ([]workspace.Tool, error) {
	s.mu.Lock()
	localTools := make([]workspace.Tool, 0, len(s.tools))
	for _, current := range s.tools {
		localTools = append(localTools, current)
	}
	mcps := make([]workspace.MCPClient, 0, len(s.mcps))
	for _, client := range s.mcps {
		mcps = append(mcps, client)
	}
	s.mu.Unlock()
	for _, client := range mcps {
		if client == nil {
			continue
		}
		if err := ensureMCPConnected(ctx, client); err != nil {
			return nil, err
		}
		tools, err := client.ListTools(ctx)
		if err != nil {
			return nil, err
		}
		localTools = append(localTools, tools...)
	}
	return localTools, nil
}

func (s *Server) findTool(ctx context.Context, name string) (workspace.Tool, error) {
	tools, err := s.allTools(ctx)
	if err != nil {
		return nil, err
	}
	for _, current := range tools {
		if current != nil && current.Name() == name {
			return current, nil
		}
	}
	return nil, fmt.Errorf("workspace/gateway: tool %q not found", name)
}

func (s *Server) getMCP(name string) workspace.MCPClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mcps[name]
}

func (s *Server) popMCP(name string) workspace.MCPClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	client := s.mcps[name]
	delete(s.mcps, name)
	return client
}

type rawMCPClient interface {
	ListRawTools(context.Context) ([]gomcp.Tool, error)
	CallTool(context.Context, string, map[string]any) (*gomcp.CallToolResult, error)
}

func listRawTools(ctx context.Context, client workspace.MCPClient) ([]gomcp.Tool, error) {
	if err := ensureMCPConnected(ctx, client); err != nil {
		return nil, err
	}
	if raw, ok := client.(interface {
		ListRawTools(context.Context) ([]gomcp.Tool, error)
	}); ok {
		return raw.ListRawTools(ctx)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	rawTools := make([]gomcp.Tool, 0, len(tools))
	for _, current := range tools {
		rawTools = append(rawTools, rawToolFromWorkspaceTool(client.Name(), current))
	}
	return rawTools, nil
}

func callRawMCPTool(ctx context.Context, client workspace.MCPClient, rawName string, input map[string]any) (*astool.ToolChunk, error) {
	if err := ensureMCPConnected(ctx, client); err != nil {
		return nil, err
	}
	if raw, ok := client.(rawMCPClient); ok {
		result, err := raw.CallTool(ctx, rawName, input)
		if err != nil {
			return nil, err
		}
		state := message.ToolResultSuccess
		if result != nil && result.IsError {
			state = message.ToolResultError
		}
		return astool.NewToolChunk(toolmcp.ConvertToolResult(result), astool.WithToolChunkState(state)), nil
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	for _, current := range tools {
		if current == nil {
			continue
		}
		if rawToolName(client.Name(), current.Name()) != rawName && current.Name() != rawName {
			continue
		}
		response, err := runGatewayTool(ctx, current, input)
		if err != nil {
			return nil, err
		}
		return astool.NewToolChunk(response.Blocks, astool.WithToolChunkState(response.State)), nil
	}
	return nil, fmt.Errorf("workspace/gateway: MCP tool %q not found", rawName)
}

func ensureMCPConnected(ctx context.Context, client workspace.MCPClient) error {
	if client == nil || !client.IsStateful() || client.IsConnected() {
		return nil
	}
	return client.Connect(ctx)
}

func runGatewayTool(ctx context.Context, current workspace.Tool, input map[string]any) (ToolCallResponse, error) {
	chunks, err := current.Execute(ctx, input, asstate.NewAgentState())
	if err != nil {
		return ToolCallResponse{}, err
	}
	response := astool.NewToolResponse()
	for chunk := range chunks {
		if appendErr := response.AppendChunk(&chunk); appendErr != nil {
			return ToolCallResponse{}, appendErr
		}
	}
	state := response.State
	if state == "" {
		state = message.ToolResultSuccess
	}
	return ToolCallResponse{State: state, Blocks: response.Content}, nil
}

func descriptorFromTool(current workspace.Tool) ToolDescriptor {
	return ToolDescriptor{
		Name:            current.Name(),
		Description:     current.Description(),
		InputSchema:     current.InputSchema(),
		MCPName:         current.MCPName(),
		ReadOnly:        current.IsReadOnly(),
		ConcurrencySafe: current.IsConcurrencySafe(),
	}
}

func rawToolFromWorkspaceTool(mcpName string, current workspace.Tool) gomcp.Tool {
	name := rawToolName(mcpName, current.Name())
	data, err := json.Marshal(current.InputSchema())
	if err != nil {
		data = nil
	}
	raw := gomcp.NewTool(
		name,
		gomcp.WithDescription(current.Description()),
		gomcp.WithRawInputSchema(data),
	)
	if current.IsReadOnly() {
		readOnly := true
		raw.Annotations.ReadOnlyHint = &readOnly
	}
	return raw
}

func rawToolName(mcpName, toolName string) string {
	prefix := "mcp__" + mcpName + "__"
	if strings.HasPrefix(toolName, prefix) {
		return strings.TrimPrefix(toolName, prefix)
	}
	return toolName
}

func configFromMCP(client workspace.MCPClient) (workspace.MCPClientConfig, error) {
	provider, ok := client.(workspace.MCPConfigProvider)
	if !ok {
		return workspace.MCPClientConfig{}, fmt.Errorf("workspace/gateway: MCP %q cannot be serialized", client.Name())
	}
	return provider.MCPClientConfig()
}

func mcpNameFromToolsPath(path string) (string, error) {
	trimmed := strings.TrimPrefix(path, "/mcps/")
	escaped := strings.TrimSuffix(trimmed, "/tools")
	name, err := url.PathUnescape(escaped)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("workspace/gateway: MCP name is empty")
	}
	return name, nil
}

func mcpAndToolNameFromCallPath(path string) (string, string, error) {
	trimmed := strings.TrimPrefix(path, "/mcps/")
	parts := strings.SplitN(trimmed, "/tools/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("workspace/gateway: invalid MCP tool path")
	}
	mcpName, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", err
	}
	toolName, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(mcpName) == "" || strings.TrimSpace(toolName) == "" {
		return "", "", fmt.Errorf("workspace/gateway: MCP name and tool name are required")
	}
	return mcpName, toolName, nil
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	if out == nil {
		return nil
	}
	maxBytes := defaultMaxRequestBytes
	if s != nil && s.maxBytes > 0 {
		maxBytes = s.maxBytes
	}
	body := http.MaxBytesReader(w, r.Body, maxBytes)
	defer body.Close()
	return json.NewDecoder(body).Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func defaultServerMCPFactory(config workspace.MCPClientConfig) (workspace.MCPClient, error) {
	opts := []toolmcp.ClientOption{toolmcp.WithStateful(config.Stateful)}
	if len(config.EnabledTools) > 0 {
		opts = append(opts, toolmcp.WithEnabledTools(config.EnabledTools...))
	}
	if len(config.DisabledTools) > 0 {
		opts = append(opts, toolmcp.WithDisabledTools(config.DisabledTools...))
	}
	if config.ExecutionTimeout > 0 {
		opts = append(opts, toolmcp.WithExecutionTimeout(config.ExecutionTimeout))
	}
	switch config.Type {
	case workspace.MCPClientTypeStdio:
		if config.Stdio == nil {
			return nil, fmt.Errorf("workspace/gateway: stdio MCP %q missing config", config.Name)
		}
		return toolmcp.NewStdioClient(config.Name, toolmcp.StdioConfig{
			Command:              config.Stdio.Command,
			Args:                 append([]string(nil), config.Stdio.Args...),
			Env:                  cloneStringMap(config.Stdio.Env),
			CWD:                  config.Stdio.CWD,
			EncodingErrorHandler: config.Stdio.EncodingErrorHandler,
		}, opts...)
	case workspace.MCPClientTypeHTTP:
		if config.HTTP == nil {
			return nil, fmt.Errorf("workspace/gateway: HTTP MCP %q missing config", config.Name)
		}
		if config.HTTP.ContinuousListening {
			opts = append(opts, toolmcp.WithStreamableHTTPContinuousListening())
		}
		return toolmcp.NewHTTPClient(config.Name, toolmcp.HTTPConfig{
			URL:       config.HTTP.URL,
			Headers:   cloneStringMap(config.HTTP.Headers),
			Timeout:   config.HTTP.Timeout,
			Transport: toolmcp.HTTPTransport(config.HTTP.Transport),
		}, opts...)
	default:
		return nil, fmt.Errorf("workspace/gateway: unsupported MCP type %q", config.Type)
	}
}
