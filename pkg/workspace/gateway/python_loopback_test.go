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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

func TestInjectedTransportUsesPythonMCPConfig(t *testing.T) {
	t.Parallel()

	var addRequest *Request
	transport := TransportFunc(func(_ context.Context, request *Request) (*Response, error) {
		switch {
		case request.Method == http.MethodPost && request.Path == "/mcps":
			addRequest = request
			return &Response{StatusCode: http.StatusCreated}, nil
		case request.Method == http.MethodGet && request.Path == "/mcps":
			return &Response{
				StatusCode: http.StatusOK,
				Body:       []byte(`[{"name":"weather","is_stateful":false,"mcp_config":{"type":"http_mcp","url":"https://example.test/mcp","timeout":1.5},"execution_timeout":2.25}]`),
			}, nil
		default:
			return nil, errors.New("unexpected request")
		}
	})
	client, err := NewClient(transport, WithPythonMCPConfigJSON(), WithMaxResponseBytes(1024))
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	config := workspace.MCPClientConfig{
		Name:             "weather",
		Type:             workspace.MCPClientTypeHTTP,
		HTTP:             &workspace.MCPHTTPConfig{URL: "https://example.test/mcp", Timeout: 1500 * time.Millisecond},
		ExecutionTimeout: 2250 * time.Millisecond,
	}
	if err := client.AddMCP(context.Background(), config); err != nil {
		t.Fatalf("AddMCP returned error: %v", err)
	}
	if addRequest == nil || addRequest.MaxResponseBytes != 1024 {
		t.Fatalf("unexpected injected transport request: %#v", addRequest)
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(addRequest.Body, &encoded); err != nil {
		t.Fatalf("decode AddMCP request: %v", err)
	}
	if _, legacy := encoded["type"]; legacy {
		t.Fatalf("Python request must not contain legacy root type: %s", addRequest.Body)
	}
	var nested struct {
		Type    string  `json:"type"`
		Timeout float64 `json:"timeout"`
	}
	if err := json.Unmarshal(encoded["mcp_config"], &nested); err != nil {
		t.Fatalf("decode nested mcp_config: %v", err)
	}
	if nested.Type != "http_mcp" || nested.Timeout != 1.5 {
		t.Fatalf("unexpected nested config: %#v", nested)
	}

	configs, err := client.ListMCPs(context.Background())
	if err != nil {
		t.Fatalf("ListMCPs returned error: %v", err)
	}
	if len(configs) != 1 || configs[0].HTTP == nil || configs[0].HTTP.Timeout != 1500*time.Millisecond || configs[0].ExecutionTimeout != 2250*time.Millisecond {
		t.Fatalf("unexpected decoded Python configs: %#v", configs)
	}
}

func TestPythonMCPConfigCanonicalAndLegacyCompatibility(t *testing.T) {
	t.Parallel()

	configs := []workspace.MCPClientConfig{{
		Name:     "files",
		Type:     workspace.MCPClientTypeStdio,
		Stateful: true,
		Stdio: &workspace.MCPStdioConfig{
			Command: "mcp-files",
			Args:    []string{"--root", "/work"},
		},
		ExecutionTimeout: 2500 * time.Millisecond,
	}}
	codec := PythonMCPCodec{}
	encoded, err := codec.Marshal(configs)
	if err != nil {
		t.Fatalf("PythonMCPCodec.Marshal returned error: %v", err)
	}
	if strings.Contains(string(encoded), `"stdio":`) || !strings.Contains(string(encoded), `"mcp_config":{"type":"stdio_mcp"`) || !strings.Contains(string(encoded), `"execution_timeout":2.5`) {
		t.Fatalf("unexpected canonical JSON: %s", encoded)
	}
	decoded, err := codec.Unmarshal(encoded)
	if err != nil {
		t.Fatalf("canonical PythonMCPCodec.Unmarshal returned error: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Stdio == nil || decoded[0].Stdio.EncodingErrorHandler != "strict" || decoded[0].ExecutionTimeout != 2500*time.Millisecond {
		t.Fatalf("unexpected canonical round trip: %#v", decoded)
	}

	legacy := []byte(`[{"name":"legacy","type":"http_mcp","is_stateful":true,"http":{"url":"https://example.test","timeout":1500000000},"execution_timeout":2250000000}]`)
	decoded, err = UnmarshalMCPConfigs(legacy)
	if err != nil {
		t.Fatalf("legacy UnmarshalMCPConfigs returned error: %v", err)
	}
	if len(decoded) != 1 || decoded[0].HTTP.Timeout != 1500*time.Millisecond || decoded[0].ExecutionTimeout != 2250*time.Millisecond {
		t.Fatalf("unexpected legacy decode: %#v", decoded)
	}
}

func TestPythonMCPConfigRejectsLossAndInvalidInput(t *testing.T) {
	t.Parallel()

	base := workspace.MCPClientConfig{
		Name: "remote",
		Type: workspace.MCPClientTypeHTTP,
		HTTP: &workspace.MCPHTTPConfig{URL: "https://example.test/mcp"},
	}
	tests := []struct {
		name   string
		mutate func(*workspace.MCPClientConfig)
		want   string
	}{
		{
			name: "go only transport",
			mutate: func(config *workspace.MCPClientConfig) {
				config.HTTP.Transport = "sse"
			},
			want: "transport cannot be encoded",
		},
		{
			name: "continuous listening",
			mutate: func(config *workspace.MCPClientConfig) {
				config.HTTP.ContinuousListening = true
			},
			want: "continuous_listening cannot be encoded",
		},
		{
			name: "negative timeout",
			mutate: func(config *workspace.MCPClientConfig) {
				config.HTTP.Timeout = -time.Second
			},
			want: "timeout must not be negative",
		},
		{
			name: "unknown type",
			mutate: func(config *workspace.MCPClientConfig) {
				config.Type = "socket_mcp"
			},
			want: "unknown MCP type",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := cloneMCPClientConfig(base)
			test.mutate(&config)
			_, err := MarshalPythonMCPConfig(config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("MarshalPythonMCPConfig error = %v, want %q", err, test.want)
			}
		})
	}

	invalidJSON := []struct {
		name string
		data string
		want string
	}{
		{
			name: "null list",
			data: `null`,
			want: "JSON array",
		},
		{
			name: "duplicates",
			data: `[{"name":"same","is_stateful":false,"mcp_config":{"type":"http_mcp","url":"https://one"}},{"name":"same","is_stateful":false,"mcp_config":{"type":"http_mcp","url":"https://two"}}]`,
			want: "duplicate MCP name",
		},
		{
			name: "unknown nested type",
			data: `[{"name":"bad","is_stateful":false,"mcp_config":{"type":"socket_mcp","url":"x"}}]`,
			want: "unknown MCP type",
		},
		{
			name: "zero duration",
			data: `[{"name":"bad","is_stateful":false,"mcp_config":{"type":"http_mcp","url":"https://one","timeout":0}}]`,
			want: "positive finite",
		},
		{
			name: "duration overflow",
			data: `[{"name":"bad","is_stateful":false,"mcp_config":{"type":"http_mcp","url":"https://one","timeout":1e30}}]`,
			want: "outside time.Duration range",
		},
		{
			name: "unknown field",
			data: `[{"name":"bad","is_stateful":false,"mcp_config":{"type":"http_mcp","url":"https://one","transport":"sse"}}]`,
			want: "unknown field",
		},
	}
	for _, test := range invalidJSON {
		t.Run(test.name, func(t *testing.T) {
			_, err := UnmarshalMCPConfigs([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("UnmarshalMCPConfigs error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPythonMCPConfigAdditionalValidation(t *testing.T) {
	t.Parallel()

	validHTTP := workspace.MCPClientConfig{
		Name: "valid_http",
		Type: workspace.MCPClientTypeHTTP,
		HTTP: &workspace.MCPHTTPConfig{URL: "https://example.test/mcp"},
	}
	if encoded, err := MarshalPythonMCPConfig(validHTTP); err != nil || !strings.Contains(string(encoded), `"mcp_config"`) {
		t.Fatalf("MarshalPythonMCPConfig valid result = %s, %v", encoded, err)
	}
	if _, err := MarshalPythonMCPConfigs([]workspace.MCPClientConfig{validHTTP, validHTTP}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate MarshalPythonMCPConfigs error = %v", err)
	}

	mutations := []struct {
		name   string
		config workspace.MCPClientConfig
		want   string
	}{
		{
			name: "invalid name",
			config: workspace.MCPClientConfig{
				Name: "bad name", Type: workspace.MCPClientTypeHTTP, HTTP: &workspace.MCPHTTPConfig{URL: "https://example.test"},
			},
			want: "invalid MCP name",
		},
		{
			name: "overlapping filters",
			config: workspace.MCPClientConfig{
				Name: "filters", Type: workspace.MCPClientTypeHTTP, HTTP: &workspace.MCPHTTPConfig{URL: "https://example.test"}, EnabledTools: []string{"echo"}, DisabledTools: []string{"echo"},
			},
			want: "overlap",
		},
		{
			name: "negative execution timeout",
			config: workspace.MCPClientConfig{
				Name: "timeout", Type: workspace.MCPClientTypeHTTP, HTTP: &workspace.MCPHTTPConfig{URL: "https://example.test"}, ExecutionTimeout: -time.Second,
			},
			want: "execution_timeout",
		},
		{
			name: "missing HTTP config",
			config: workspace.MCPClientConfig{
				Name: "missing", Type: workspace.MCPClientTypeHTTP,
			},
			want: "only HTTP config",
		},
		{
			name: "empty HTTP URL",
			config: workspace.MCPClientConfig{
				Name: "empty_url", Type: workspace.MCPClientTypeHTTP, HTTP: &workspace.MCPHTTPConfig{},
			},
			want: "URL is empty",
		},
		{
			name: "stateless stdio",
			config: workspace.MCPClientConfig{
				Name: "stdio", Type: workspace.MCPClientTypeStdio, Stdio: &workspace.MCPStdioConfig{Command: "server"},
			},
			want: "must be stateful",
		},
		{
			name: "empty stdio command",
			config: workspace.MCPClientConfig{
				Name: "stdio", Type: workspace.MCPClientTypeStdio, Stateful: true, Stdio: &workspace.MCPStdioConfig{},
			},
			want: "command is empty",
		},
		{
			name: "invalid encoding handler",
			config: workspace.MCPClientConfig{
				Name: "stdio", Type: workspace.MCPClientTypeStdio, Stateful: true, Stdio: &workspace.MCPStdioConfig{Command: "server", EncodingErrorHandler: "skip"},
			},
			want: "encoding_error_handler",
		},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			_, err := MarshalPythonMCPConfig(test.config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("MarshalPythonMCPConfig error = %v, want %q", err, test.want)
			}
		})
	}

	decoded, err := UnmarshalMCPConfigs([]byte(`[{"name":"defaults","is_stateful":false,"mcp_config":{"type":"http_mcp","url":"https://example.test"}}]`))
	if err != nil || len(decoded) != 1 || decoded[0].HTTP.Timeout != pythonDefaultHTTPTimeout {
		t.Fatalf("Python default timeout decode = %#v, %v", decoded, err)
	}
	if _, err := UnmarshalMCPConfigs([]byte(`[{"name":"legacy","type":"http_mcp","is_stateful":false,"http":{"url":"https://example.test","timeout":-1}}]`)); err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("negative legacy timeout error = %v", err)
	}
	if _, err := UnmarshalMCPConfigs([]byte(`[] {}`)); err == nil {
		t.Fatalf("trailing JSON should fail")
	}
}

func TestDecodeLoopbackResponseInlineAndBodyFile(t *testing.T) {
	t.Parallel()

	inline := `{"status":200,"headers":{"Content-Type":["application/json"]},"body":"` + base64.StdEncoding.EncodeToString([]byte(`{"ok":true}`)) + `"}`
	response, err := DecodeLoopbackResponse(context.Background(), []byte(inline), 1024, nil)
	if err != nil {
		t.Fatalf("DecodeLoopbackResponse inline returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" || string(response.Body) != `{"ok":true}` {
		t.Fatalf("unexpected inline response: %#v", response)
	}
	padded, err := DecodeLoopbackResponse(context.Background(), []byte(`{"status":200,"body":"YWI="}`), 2, nil)
	if err != nil || string(padded.Body) != "ab" {
		t.Fatalf("padded body at exact limit = %#v, %v", padded, err)
	}

	var readPath string
	response, err = DecodeLoopbackResponse(
		context.Background(),
		[]byte(`{"status":404,"body_file":"/tmp/response.bin"}`),
		1024,
		func(_ context.Context, filePath string, maxBytes int64) ([]byte, error) {
			readPath = filePath
			if maxBytes != 1024 {
				t.Fatalf("body reader maxBytes = %d", maxBytes)
			}
			return []byte("missing"), nil
		},
	)
	if err != nil {
		t.Fatalf("DecodeLoopbackResponse body_file returned error: %v", err)
	}
	if readPath != "/tmp/response.bin" || response.StatusCode != http.StatusNotFound || string(response.Body) != "missing" {
		t.Fatalf("unexpected body-file response: path=%q response=%#v", readPath, response)
	}
}

func TestDecodeLoopbackResponseRejectsInvalidEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		limit   int64
		reader  BodyFileReader
		want    string
	}{
		{name: "invalid status", payload: `{"status":0,"body":""}`, limit: 10, want: "invalid status"},
		{name: "both body sources", payload: `{"status":200,"body":"","body_file":"/tmp/x"}`, limit: 10, want: "exactly one"},
		{name: "invalid base64", payload: `{"status":200,"body":"***"}`, limit: 10, want: "valid base64"},
		{name: "oversize inline", payload: `{"status":200,"body":"YWJj"}`, limit: 2, want: "exceeds"},
		{name: "relative file", payload: `{"status":200,"body_file":"tmp/x"}`, limit: 10, want: "clean and absolute"},
		{name: "missing reader", payload: `{"status":200,"body_file":"/tmp/x"}`, limit: 10, want: "body-file reader"},
		{name: "oversize file", payload: `{"status":200,"body_file":"/tmp/x"}`, limit: 2, reader: func(context.Context, string, int64) ([]byte, error) { return []byte("abc"), nil }, want: "exceeds"},
		{name: "header injection", payload: `{"status":200,"headers":{"X-Test":["ok\r\nInjected: yes"]},"body":""}`, limit: 100, want: "invalid header value"},
		{name: "unknown field", payload: `{"status":200,"body":"","token":"secret"}`, limit: 100, want: "unknown field"},
		{name: "mixed error", payload: `{"status":-1,"error":"failed","body":""}`, limit: 100, want: "invalid loopback error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeLoopbackResponse(context.Background(), []byte(test.payload), test.limit, test.reader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeLoopbackResponse error = %v, want %q", err, test.want)
			}
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeLoopbackResponse(canceled, []byte(`{"status":200,"body":""}`), 100, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled DecodeLoopbackResponse error = %v", err)
	}
}

func TestDecodeLoopbackResponseAdditionalBoundaries(t *testing.T) {
	t.Parallel()

	_, err := DecodeLoopbackResponse(nil, nil, 1, nil) //nolint:staticcheck // Verify explicit nil-context validation.
	if err == nil || !strings.Contains(err.Error(), "nil context") {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := DecodeLoopbackResponse(context.Background(), []byte(`{"status":200,"body":""}`), 0, nil); err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("zero limit error = %v", err)
	}
	if _, err := DecodeLoopbackResponse(context.Background(), make([]byte, maxResponseHeaderBytes+3), 1, nil); err == nil || !strings.Contains(err.Error(), "envelope exceeds") {
		t.Fatalf("oversized envelope error = %v", err)
	}
	response, err := DecodeLoopbackResponse(context.Background(), []byte(`{"status":200,"body":""}`), math.MaxInt64, nil)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("max limit response = %#v, %v", response, err)
	}

	_, err = DecodeLoopbackResponse(context.Background(), []byte(`{"status":-1,"error":"  timeout\nwhile dialing  "}`), 100, nil)
	if err == nil || !strings.Contains(err.Error(), "timeout while dialing") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("sanitized loopback error = %v", err)
	}
	if _, err := DecodeLoopbackResponse(context.Background(), []byte(`{"status":200,"error":"bad","body":""}`), 100, nil); err == nil || !strings.Contains(err.Error(), "contains an error") {
		t.Fatalf("success error-envelope validation = %v", err)
	}
	if _, err := DecodeLoopbackResponse(context.Background(), []byte(`{"status":200}`), 100, nil); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("missing body validation = %v", err)
	}
	if _, err := DecodeLoopbackResponse(context.Background(), []byte(`{"status":200,"body_file":"/tmp/x"}`), 100, func(context.Context, string, int64) ([]byte, error) {
		return nil, errors.New("read failed")
	}); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("body reader error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	if _, err := DecodeLoopbackResponse(canceled, []byte(`{"status":200,"body_file":"/tmp/x"}`), 100, func(context.Context, string, int64) ([]byte, error) {
		cancel()
		return nil, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-read cancellation error = %v", err)
	}

	headers := make(map[string][]string, maxResponseHeaderCount+1)
	for index := 0; index <= maxResponseHeaderCount; index++ {
		headers[fmt.Sprintf("X-%d", index)] = []string{"value"}
	}
	payload, err := json.Marshal(loopbackEnvelope{Status: 200, Header: headers, Body: pointerTo("")})
	if err != nil {
		t.Fatalf("marshal many headers: %v", err)
	}
	if _, err := DecodeLoopbackResponse(context.Background(), payload, 100, nil); err == nil || !strings.Contains(err.Error(), "too many headers") {
		t.Fatalf("many header validation error = %v", err)
	}
	if _, err := DecodeLoopbackResponse(context.Background(), []byte(`{"status":200,"headers":{"Bad Header":["value"]},"body":""}`), 100, nil); err == nil || !strings.Contains(err.Error(), "header name") {
		t.Fatalf("invalid header name error = %v", err)
	}
}

func pointerTo[T any](value T) *T {
	return &value
}

func TestClientTransportResponseValidationAndContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		response  *Response
		transport error
		want      string
	}{
		{name: "nil response", want: "nil response"},
		{name: "invalid status", response: &Response{StatusCode: 999}, want: "invalid status"},
		{name: "large body", response: &Response{StatusCode: http.StatusOK, Body: []byte("abc")}, want: "exceeds"},
		{name: "bad header", response: &Response{StatusCode: http.StatusOK, Header: http.Header{"X": {"bad\r\nvalue"}}}, want: "invalid header value"},
		{name: "transport error", transport: errors.New("boom"), want: "boom"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(TransportFunc(func(context.Context, *Request) (*Response, error) {
				return test.response, test.transport
			}), WithMaxResponseBytes(2))
			if err != nil {
				t.Fatalf("NewClient returned error: %v", err)
			}
			err = client.Health(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Health error = %v, want %q", err, test.want)
			}
		})
	}

	client, err := NewClient(TransportFunc(func(context.Context, *Request) (*Response, error) {
		t.Fatal("transport must not run for a canceled context")
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Health(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Health error = %v", err)
	}

	client, err = NewClient(
		TransportFunc(func(context.Context, *Request) (*Response, error) {
			t.Fatal("transport must not receive an injected header")
			return nil, nil
		}),
		WithHeaders(map[string]string{"X-Test": "safe\r\nInjected: yes"}),
	)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if err := client.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid header value") || strings.Contains(err.Error(), "Injected") {
		t.Fatalf("request header validation error = %v", err)
	}

	if _, err := NewClient(nil); err == nil || !strings.Contains(err.Error(), "nil transport") {
		t.Fatalf("NewClient(nil) error = %v", err)
	}
	var nilTransport TransportFunc
	if _, err := nilTransport.RoundTrip(context.Background(), &Request{}); err == nil || !strings.Contains(err.Error(), "nil transport function") {
		t.Fatalf("nil TransportFunc error = %v", err)
	}
}
