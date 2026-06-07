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
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	goclient "github.com/mark3labs/mcp-go/client"
	gotransport "github.com/mark3labs/mcp-go/client/transport"
	mcpserver "github.com/mark3labs/mcp-go/server"

	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

// HTTPTransport selects the MCP HTTP transport implementation.
type HTTPTransport string

const (
	// HTTPTransportAuto selects SSE for /sse and /messages/ URLs, otherwise streamable HTTP.
	HTTPTransportAuto HTTPTransport = "auto"
	// HTTPTransportSSE uses the MCP SSE transport.
	HTTPTransportSSE HTTPTransport = "sse"
	// HTTPTransportStreamable uses the MCP streamable HTTP transport.
	HTTPTransportStreamable HTTPTransport = "streamable_http"
)

// OAuthConfig configures OAuth for HTTP MCP transports. It is a runtime-only
// option because token stores and HTTP clients cannot be serialized into
// workspace .mcp indexes.
type OAuthConfig = goclient.OAuthConfig

// StdioConfig describes a subprocess-backed MCP server.
type StdioConfig struct {
	Command              string            `json:"command"`
	Args                 []string          `json:"args,omitempty"`
	Env                  map[string]string `json:"env,omitempty"`
	CWD                  string            `json:"cwd,omitempty"`
	EncodingErrorHandler string            `json:"encoding_error_handler,omitempty"`
}

// HTTPConfig describes an HTTP MCP server.
type HTTPConfig struct {
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Timeout   time.Duration     `json:"timeout,omitempty"`
	Transport HTTPTransport     `json:"transport,omitempty"`
}

// WithStreamableHTTPContinuousListening enables streamable HTTP's standalone
// notification listener. It has no effect for stdio, in-process, or SSE
// transports.
func WithStreamableHTTPContinuousListening() ClientOption {
	return func(options *clientOptions) {
		options.continuousListening = true
	}
}

// WithOAuthConfig enables OAuth support for HTTP MCP transports. OAuth settings
// are not persisted by MCPClientConfig; callers that restore clients from .mcp
// need to re-apply this option in their factory.
func WithOAuthConfig(config OAuthConfig) ClientOption {
	return func(options *clientOptions) {
		config.Scopes = append([]string(nil), config.Scopes...)
		options.oauthConfig = &config
	}
}

type clientFactory func(context.Context) (*goclient.Client, error)

// NewStdioClient creates an MCP client backed by a stdio subprocess.
func NewStdioClient(name string, config StdioConfig, opts ...ClientOption) (*Client, error) {
	options := defaultClientOptions()
	for _, opt := range opts {
		opt(&options)
	}
	if !options.stateful {
		return nil, fmt.Errorf("mcp: stdio MCP client %q must be stateful", name)
	}
	if err := validateStdioConfig(config); err != nil {
		return nil, err
	}
	configSnapshot := asworkspace.MCPClientConfig{
		Name:             strings.TrimSpace(name),
		Type:             asworkspace.MCPClientTypeStdio,
		Stateful:         options.stateful,
		Stdio:            &asworkspace.MCPStdioConfig{Command: config.Command, Args: append([]string(nil), config.Args...), Env: cloneStringMap(config.Env), CWD: config.CWD, EncodingErrorHandler: config.EncodingErrorHandler},
		EnabledTools:     append([]string(nil), options.enabledTools...),
		DisabledTools:    append([]string(nil), options.disabledTools...),
		ExecutionTimeout: options.executionTimeout,
	}
	return newClient(name, options, configSnapshot, func(context.Context) (*goclient.Client, error) {
		stdioOptions := []gotransport.StdioOption{}
		if strings.TrimSpace(config.CWD) != "" {
			stdioOptions = append(stdioOptions, gotransport.WithCommandFunc(func(ctx context.Context, command string, env, args []string) (*exec.Cmd, error) {
				cmd := exec.CommandContext(ctx, command, args...)
				cmd.Env = append(os.Environ(), env...)
				cmd.Dir = config.CWD
				return cmd, nil
			}))
		}
		transport := gotransport.NewStdioWithOptions(config.Command, envList(config.Env), append([]string(nil), config.Args...), stdioOptions...)
		return goclient.NewClient(transport), nil
	})
}

// NewHTTPClient creates an MCP client backed by SSE or streamable HTTP.
func NewHTTPClient(name string, config HTTPConfig, opts ...ClientOption) (*Client, error) {
	options := defaultClientOptions()
	for _, opt := range opts {
		opt(&options)
	}
	if err := validateHTTPConfig(config); err != nil {
		return nil, err
	}
	configSnapshot := asworkspace.MCPClientConfig{
		Name:             strings.TrimSpace(name),
		Type:             asworkspace.MCPClientTypeHTTP,
		Stateful:         options.stateful,
		HTTP:             &asworkspace.MCPHTTPConfig{URL: config.URL, Headers: cloneStringMap(config.Headers), Timeout: config.Timeout, Transport: string(config.Transport), ContinuousListening: options.continuousListening},
		EnabledTools:     append([]string(nil), options.enabledTools...),
		DisabledTools:    append([]string(nil), options.disabledTools...),
		ExecutionTimeout: options.executionTimeout,
	}
	return newClient(name, options, configSnapshot, func(context.Context) (*goclient.Client, error) {
		switch resolveHTTPTransport(config) {
		case HTTPTransportSSE:
			sseOptions := []gotransport.ClientOption{}
			if len(config.Headers) > 0 {
				sseOptions = append(sseOptions, gotransport.WithHeaders(cloneStringMap(config.Headers)))
			}
			if config.Timeout > 0 {
				sseOptions = append(sseOptions, gotransport.WithHTTPClient(&http.Client{Timeout: config.Timeout}))
			}
			if options.oauthConfig != nil {
				return goclient.NewOAuthSSEClient(config.URL, *options.oauthConfig, sseOptions...)
			}
			return goclient.NewSSEMCPClient(config.URL, sseOptions...)
		default:
			httpOptions := []gotransport.StreamableHTTPCOption{}
			if len(config.Headers) > 0 {
				httpOptions = append(httpOptions, gotransport.WithHTTPHeaders(cloneStringMap(config.Headers)))
			}
			if config.Timeout > 0 {
				httpOptions = append(httpOptions, gotransport.WithHTTPTimeout(config.Timeout))
			}
			if options.continuousListening {
				httpOptions = append(httpOptions, gotransport.WithContinuousListening())
			}
			if options.oauthConfig != nil {
				return goclient.NewOAuthStreamableHttpClient(config.URL, *options.oauthConfig, httpOptions...)
			}
			return goclient.NewStreamableHttpClient(config.URL, httpOptions...)
		}
	})
}

// NewInProcessClient creates an MCP client for an in-process MCP server.
func NewInProcessClient(name string, server *mcpserver.MCPServer, opts ...ClientOption) (*Client, error) {
	options := defaultClientOptions()
	for _, opt := range opts {
		opt(&options)
	}
	if server == nil {
		return nil, fmt.Errorf("mcp: in-process server is required")
	}
	return newClient(name, options, asworkspace.MCPClientConfig{}, func(context.Context) (*goclient.Client, error) {
		return goclient.NewInProcessClient(server)
	})
}

func validateStdioConfig(config StdioConfig) error {
	if strings.TrimSpace(config.Command) == "" {
		return fmt.Errorf("mcp: stdio command is required")
	}
	switch config.EncodingErrorHandler {
	case "", "strict", "ignore", "replace":
		return nil
	default:
		return fmt.Errorf("mcp: unsupported stdio encoding error handler %q", config.EncodingErrorHandler)
	}
}

func validateHTTPConfig(config HTTPConfig) error {
	if strings.TrimSpace(config.URL) == "" {
		return fmt.Errorf("mcp: HTTP URL is required")
	}
	switch config.Transport {
	case "", HTTPTransportAuto, HTTPTransportSSE, HTTPTransportStreamable:
		return nil
	default:
		return fmt.Errorf("mcp: unsupported HTTP transport %q", config.Transport)
	}
}

func resolveHTTPTransport(config HTTPConfig) HTTPTransport {
	if config.Transport != "" && config.Transport != HTTPTransportAuto {
		return config.Transport
	}
	if strings.HasSuffix(config.URL, "/sse") || strings.HasSuffix(config.URL, "/messages/") {
		return HTTPTransportSSE
	}
	return HTTPTransportStreamable
}

func envList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
