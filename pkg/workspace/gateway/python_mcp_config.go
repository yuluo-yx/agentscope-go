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
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

const pythonDefaultHTTPTimeout = 30 * time.Second

type pythonMCPConfig struct {
	Name             string          `json:"name"`
	Stateful         bool            `json:"is_stateful"`
	MCPConfig        json.RawMessage `json:"mcp_config"`
	EnabledTools     []string        `json:"enable_tools,omitempty"`
	DisabledTools    []string        `json:"disable_tools,omitempty"`
	ExecutionTimeout *float64        `json:"execution_timeout,omitempty"`
}

type pythonMCPType struct {
	Type workspace.MCPClientType `json:"type"`
}

type pythonStdioMCPConfig struct {
	Type                 workspace.MCPClientType `json:"type"`
	Command              string                  `json:"command"`
	Args                 []string                `json:"args,omitempty"`
	Env                  map[string]string       `json:"env,omitempty"`
	CWD                  string                  `json:"cwd,omitempty"`
	EncodingErrorHandler string                  `json:"encoding_error_handler,omitempty"`
}

type pythonHTTPMCPConfig struct {
	Type    workspace.MCPClientType `json:"type"`
	URL     string                  `json:"url"`
	Headers map[string]string       `json:"headers,omitempty"`
	Timeout *float64                `json:"timeout,omitempty"`
}

// PythonMCPCodec reads shared .mcp files and writes AgentScope Python's canonical format.
type PythonMCPCodec struct{}

// Marshal encodes a canonical Python .mcp JSON array.
func (PythonMCPCodec) Marshal(configs []workspace.MCPClientConfig) ([]byte, error) {
	return MarshalPythonMCPConfigs(configs)
}

// Unmarshal reads a canonical Python or legacy Go .mcp JSON array.
func (PythonMCPCodec) Unmarshal(data []byte) ([]workspace.MCPClientConfig, error) {
	return UnmarshalMCPConfigs(data)
}

// MarshalPythonMCPConfig encodes one config using AgentScope Python's canonical
// mcp_config shape. Python has no continuous-listening or custom-transport
// fields, so non-zero Go-only values fail instead of losing semantics silently.
func MarshalPythonMCPConfig(config workspace.MCPClientConfig) ([]byte, error) {

	pythonConfig, err := toPythonMCPConfig(config)
	if err != nil {
		return nil, err
	}

	return json.Marshal(pythonConfig)
}

// MarshalPythonMCPConfigs encodes the JSON array used by a Python workspace .mcp file.
func MarshalPythonMCPConfigs(configs []workspace.MCPClientConfig) ([]byte, error) {

	if err := validateUniqueMCPNames(configs); err != nil {
		return nil, err
	}

	pythonConfigs := make([]pythonMCPConfig, 0, len(configs))
	for _, config := range configs {
		pythonConfig, err := toPythonMCPConfig(config)
		if err != nil {
			return nil, err
		}
		pythonConfigs = append(pythonConfigs, pythonConfig)
	}

	return json.Marshal(pythonConfigs)
}

// UnmarshalMCPConfigs reads a canonical Python .mcp array and legacy Go entries
// with root-level type/stdio/http fields. Writes should use the canonical encoder.
func UnmarshalMCPConfigs(data []byte) ([]workspace.MCPClientConfig, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, fmt.Errorf("workspace/gateway: MCP config must be a JSON array")
	}

	var entries []json.RawMessage
	if err := decodeStrictJSON(data, &entries); err != nil {
		return nil, fmt.Errorf("workspace/gateway: decode MCP config list: %w", err)
	}

	configs := make([]workspace.MCPClientConfig, 0, len(entries))
	for index, entry := range entries {
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(entry, &shape); err != nil {
			return nil, fmt.Errorf("workspace/gateway: decode MCP config %d: %w", index, err)
		}

		var (
			config workspace.MCPClientConfig
			err    error
		)
		if _, canonical := shape["mcp_config"]; canonical {
			config, err = fromPythonMCPConfig(entry)
		} else {
			config, err = fromLegacyMCPConfig(entry)
		}
		if err != nil {
			return nil, fmt.Errorf("workspace/gateway: decode MCP config %d: %w", index, err)
		}
		configs = append(configs, config)
	}

	if err := validateUniqueMCPNames(configs); err != nil {
		return nil, err
	}

	return configs, nil
}

func toPythonMCPConfig(config workspace.MCPClientConfig) (pythonMCPConfig, error) {

	if err := validateMCPConfig(config); err != nil {
		return pythonMCPConfig{}, err
	}

	var nested any
	switch config.Type {
	case workspace.MCPClientTypeStdio:
		stdio := config.Stdio
		nested = pythonStdioMCPConfig{
			Type:                 workspace.MCPClientTypeStdio,
			Command:              stdio.Command,
			Args:                 cloneStrings(stdio.Args),
			Env:                  cloneStringMap(stdio.Env),
			CWD:                  stdio.CWD,
			EncodingErrorHandler: defaultEncodingErrorHandler(stdio.EncodingErrorHandler),
		}
	case workspace.MCPClientTypeHTTP:
		httpConfig := config.HTTP
		if httpConfig.Transport != "" {
			return pythonMCPConfig{}, fmt.Errorf("workspace/gateway: MCP %q transport cannot be encoded for Python", config.Name)
		}
		if httpConfig.ContinuousListening {
			return pythonMCPConfig{}, fmt.Errorf("workspace/gateway: MCP %q continuous_listening cannot be encoded for Python", config.Name)
		}
		timeout, err := encodeOptionalDuration(httpConfig.Timeout, "timeout")
		if err != nil {
			return pythonMCPConfig{}, err
		}
		nested = pythonHTTPMCPConfig{
			Type:    workspace.MCPClientTypeHTTP,
			URL:     httpConfig.URL,
			Headers: cloneStringMap(httpConfig.Headers),
			Timeout: timeout,
		}
	default:
		return pythonMCPConfig{}, fmt.Errorf("workspace/gateway: unknown MCP type %q", config.Type)
	}

	nestedJSON, err := json.Marshal(nested)
	if err != nil {
		return pythonMCPConfig{}, err
	}
	executionTimeout, err := encodeOptionalDuration(config.ExecutionTimeout, "execution_timeout")
	if err != nil {
		return pythonMCPConfig{}, err
	}

	return pythonMCPConfig{
		Name:             config.Name,
		Stateful:         config.Stateful,
		MCPConfig:        nestedJSON,
		EnabledTools:     cloneStrings(config.EnabledTools),
		DisabledTools:    cloneStrings(config.DisabledTools),
		ExecutionTimeout: executionTimeout,
	}, nil
}

func fromPythonMCPConfig(data []byte) (workspace.MCPClientConfig, error) {

	var raw pythonMCPConfig
	if err := decodeStrictJSON(data, &raw); err != nil {
		return workspace.MCPClientConfig{}, err
	}

	var discriminator pythonMCPType
	if err := json.Unmarshal(raw.MCPConfig, &discriminator); err != nil {
		return workspace.MCPClientConfig{}, fmt.Errorf("decode mcp_config type: %w", err)
	}

	config := workspace.MCPClientConfig{
		Name:          raw.Name,
		Type:          discriminator.Type,
		Stateful:      raw.Stateful,
		EnabledTools:  cloneStrings(raw.EnabledTools),
		DisabledTools: cloneStrings(raw.DisabledTools),
	}
	var err error
	config.ExecutionTimeout, err = decodeOptionalDuration(raw.ExecutionTimeout, "execution_timeout", 0)
	if err != nil {
		return workspace.MCPClientConfig{}, err
	}

	switch discriminator.Type {
	case workspace.MCPClientTypeStdio:
		var stdio pythonStdioMCPConfig
		if err := decodeStrictJSON(raw.MCPConfig, &stdio); err != nil {
			return workspace.MCPClientConfig{}, err
		}
		config.Stdio = &workspace.MCPStdioConfig{
			Command:              stdio.Command,
			Args:                 cloneStrings(stdio.Args),
			Env:                  cloneStringMap(stdio.Env),
			CWD:                  stdio.CWD,
			EncodingErrorHandler: defaultEncodingErrorHandler(stdio.EncodingErrorHandler),
		}
	case workspace.MCPClientTypeHTTP:
		var httpConfig pythonHTTPMCPConfig
		if err := decodeStrictJSON(raw.MCPConfig, &httpConfig); err != nil {
			return workspace.MCPClientConfig{}, err
		}
		timeout, err := decodeOptionalDuration(httpConfig.Timeout, "timeout", pythonDefaultHTTPTimeout)
		if err != nil {
			return workspace.MCPClientConfig{}, err
		}
		config.HTTP = &workspace.MCPHTTPConfig{
			URL:     httpConfig.URL,
			Headers: cloneStringMap(httpConfig.Headers),
			Timeout: timeout,
		}
	default:
		return workspace.MCPClientConfig{}, fmt.Errorf("unknown MCP type %q", discriminator.Type)
	}

	if err := validateMCPConfig(config); err != nil {
		return workspace.MCPClientConfig{}, err
	}

	return config, nil
}

func fromLegacyMCPConfig(data []byte) (workspace.MCPClientConfig, error) {

	type legacyMCPConfig workspace.MCPClientConfig
	var raw legacyMCPConfig
	if err := decodeStrictJSON(data, &raw); err != nil {
		return workspace.MCPClientConfig{}, err
	}
	config := workspace.MCPClientConfig(raw)
	if err := validateMCPConfig(config); err != nil {
		return workspace.MCPClientConfig{}, err
	}

	return cloneMCPClientConfig(config), nil
}

func validateMCPConfig(config workspace.MCPClientConfig) error {

	if !validMCPName(config.Name) {
		return fmt.Errorf("workspace/gateway: invalid MCP name")
	}
	if err := validateToolFilters(config.EnabledTools, config.DisabledTools); err != nil {
		return err
	}
	if config.ExecutionTimeout < 0 {
		return fmt.Errorf("workspace/gateway: execution_timeout must not be negative")
	}

	switch config.Type {
	case workspace.MCPClientTypeStdio:
		if config.Stdio == nil || config.HTTP != nil {
			return fmt.Errorf("workspace/gateway: stdio MCP must contain only stdio config")
		}
		if !config.Stateful {
			return fmt.Errorf("workspace/gateway: stdio MCP must be stateful")
		}
		if strings.TrimSpace(config.Stdio.Command) == "" {
			return fmt.Errorf("workspace/gateway: stdio MCP command is empty")
		}
		if !validEncodingErrorHandler(config.Stdio.EncodingErrorHandler) {
			return fmt.Errorf("workspace/gateway: invalid encoding_error_handler")
		}
	case workspace.MCPClientTypeHTTP:
		if config.HTTP == nil || config.Stdio != nil {
			return fmt.Errorf("workspace/gateway: HTTP MCP must contain only HTTP config")
		}
		if strings.TrimSpace(config.HTTP.URL) == "" {
			return fmt.Errorf("workspace/gateway: HTTP MCP URL is empty")
		}
		if config.HTTP.Timeout < 0 {
			return fmt.Errorf("workspace/gateway: timeout must not be negative")
		}
	default:
		return fmt.Errorf("workspace/gateway: unknown MCP type %q", config.Type)
	}

	return nil
}

func validateUniqueMCPNames(configs []workspace.MCPClientConfig) error {

	names := make(map[string]struct{}, len(configs))
	for _, config := range configs {
		if _, exists := names[config.Name]; exists {
			return fmt.Errorf("workspace/gateway: duplicate MCP name %q", config.Name)
		}
		names[config.Name] = struct{}{}
	}

	return nil
}

func validateToolFilters(enabled, disabled []string) error {

	enabledSet := make(map[string]struct{}, len(enabled))
	for _, name := range enabled {
		enabledSet[name] = struct{}{}
	}
	for _, name := range disabled {
		if _, exists := enabledSet[name]; exists {
			return fmt.Errorf("workspace/gateway: enable_tools and disable_tools overlap")
		}
	}

	return nil
}

func validMCPName(name string) bool {

	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		char := name[i]
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			continue
		}
		return false
	}

	return true
}

func validEncodingErrorHandler(handler string) bool {

	switch handler {
	case "", "strict", "ignore", "replace":
		return true
	default:
		return false
	}
}

func defaultEncodingErrorHandler(handler string) string {

	if handler == "" {
		return "strict"
	}

	return handler
}

func encodeOptionalDuration(duration time.Duration, field string) (*float64, error) {

	if duration < 0 {
		return nil, fmt.Errorf("workspace/gateway: %s must not be negative", field)
	}
	if duration == 0 {
		return nil, nil
	}
	seconds := float64(duration) / float64(time.Second)

	return &seconds, nil
}

func decodeOptionalDuration(seconds *float64, field string, defaultValue time.Duration) (time.Duration, error) {

	if seconds == nil {
		return defaultValue, nil
	}
	if math.IsNaN(*seconds) || math.IsInf(*seconds, 0) || *seconds <= 0 {
		return 0, fmt.Errorf("workspace/gateway: %s must be a positive finite number of seconds", field)
	}
	nanoseconds := *seconds * float64(time.Second)
	if nanoseconds > float64(math.MaxInt64) || nanoseconds < 1 {
		return 0, fmt.Errorf("workspace/gateway: %s is outside time.Duration range", field)
	}
	duration := time.Duration(math.Round(nanoseconds))
	if duration <= 0 {
		return 0, fmt.Errorf("workspace/gateway: %s is outside time.Duration range", field)
	}

	return duration, nil
}

func decodeStrictJSON(data []byte, out any) error {

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON")
		}
		return err
	}

	return nil
}

func cloneStrings(values []string) []string {

	return append([]string(nil), values...)
}
