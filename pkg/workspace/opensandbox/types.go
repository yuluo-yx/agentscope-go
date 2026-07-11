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

// Package opensandbox provides remote workspaces managed by OpenSandbox.
package opensandbox

import (
	"context"
	"time"

	sdk "github.com/alibaba/OpenSandbox/sdks/sandbox/go"

	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

const (
	defaultImage          = "python:3.11-slim"
	defaultProtocol       = "http"
	defaultRequestTimeout = 10 * time.Minute
	defaultSandboxTimeout = 5 * time.Minute
	defaultGatewayPort    = 5600
	defaultWorkdir        = "/workspace"
	defaultGatewayHome    = "/root/.agentscope"
	metadataWorkspaceID   = "agentscope.workspace.id"
	pythonAgentScope      = "agentscope==2.0.4"
)

// ResourceLimits aliases the OpenSandbox SDK resource limit map.
type ResourceLimits = sdk.ResourceLimits

// NetworkPolicy aliases the OpenSandbox SDK network policy.
type NetworkPolicy = sdk.NetworkPolicy

// NetworkRule aliases one OpenSandbox SDK network rule.
type NetworkRule = sdk.NetworkRule

type config struct {
	id                  string
	image               string
	apiKey              string
	domain              string
	protocol            string
	requestTimeout      time.Duration
	sandboxTimeout      time.Duration
	gatewayPort         int
	env                 map[string]string
	metadata            map[string]string
	resourceLimits      sdk.ResourceLimits
	entrypoint          []string
	networkPolicy       *sdk.NetworkPolicy
	extraPythonPackages []string
	instructions        string
	defaultMCPs         []workspace.MCPClient
	skillPaths          []string
	runtime             sandboxRuntime
}

type sandboxSpec struct {
	ID             string
	Image          string
	Connection     sdk.ConnectionConfig
	Timeout        time.Duration
	Env            map[string]string
	Metadata       map[string]string
	ResourceLimits sdk.ResourceLimits
	Entrypoint     []string
	NetworkPolicy  *sdk.NetworkPolicy
}

type sandboxInfo struct {
	ID        string
	State     sdk.SandboxState
	CreatedAt time.Time
}

type runResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type sandboxRuntime interface {
	List(context.Context, sdk.ConnectionConfig, string) ([]sandboxInfo, error)
	Create(context.Context, sandboxSpec) (sandboxHandle, error)
	Connect(context.Context, sdk.ConnectionConfig, string, time.Duration) (sandboxHandle, error)
	Resume(context.Context, sdk.ConnectionConfig, string, time.Duration) (sandboxHandle, error)
}

type sandboxHandle interface {
	ID() string
	Healthy(context.Context) (bool, error)
	Run(context.Context, []string, string, map[string]string, time.Duration) (runResult, error)
	ReadFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte) error
	Pause(context.Context) error
	Close() error
}
