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

package opensandbox

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	sdk "github.com/alibaba/OpenSandbox/sdks/sandbox/go"

	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Option configures an OpenSandbox Workspace.
type Option func(*config) error

// WithWorkspaceID sets the stable identifier used to resume a remote Sandbox across processes.
func WithWorkspaceID(id string) Option {
	return func(config *config) error {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("workspace/opensandbox: workspace id is empty")
		}
		config.id = id
		return nil
	}
}

// WithImage sets the container image used when creating a new Sandbox.
func WithImage(image string) Option {
	return func(config *config) error {
		image = strings.TrimSpace(image)
		if image == "" {
			return fmt.Errorf("workspace/opensandbox: image is empty")
		}
		config.image = image
		return nil
	}
}

// WithAPIKey sets the OpenSandbox API key. The SDK reads the environment when it is empty.
func WithAPIKey(apiKey string) Option {
	return func(config *config) error {
		config.apiKey = strings.TrimSpace(apiKey)
		return nil
	}
}

// WithDomain sets the OpenSandbox service address. The SDK reads the environment when it is empty.
func WithDomain(domain string) Option {
	return func(config *config) error {
		config.domain = strings.TrimSpace(domain)
		return nil
	}
}

// WithProtocol sets the OpenSandbox service protocol and accepts only HTTP or HTTPS.
func WithProtocol(protocol string) Option {
	return func(config *config) error {
		protocol = strings.ToLower(strings.TrimSpace(protocol))
		if protocol != "http" && protocol != "https" {
			return fmt.Errorf("workspace/opensandbox: protocol must be http or https")
		}
		config.protocol = protocol
		return nil
	}
}

// WithRequestTimeout sets the timeout for one OpenSandbox SDK request.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(config *config) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/opensandbox: request timeout must be positive")
		}
		config.requestTimeout = timeout
		return nil
	}
}

// WithTimeout sets the new Sandbox TTL and the connect or resume readiness timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(config *config) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/opensandbox: timeout must be positive")
		}
		config.sandboxTimeout = timeout
		return nil
	}
}

// WithGatewayPort sets the loopback port for the in-sandbox Python MCP gateway.
func WithGatewayPort(port int) Option {
	return func(config *config) error {
		if port <= 0 || port > 65535 {
			return fmt.Errorf("workspace/opensandbox: gateway port must be between 1 and 65535")
		}
		config.gatewayPort = port
		return nil
	}
}

// WithEnv sets one environment variable injected when a new Sandbox is created.
func WithEnv(name, value string) Option {
	return func(config *config) error {
		name = strings.TrimSpace(name)
		if !environmentNamePattern.MatchString(name) {
			return fmt.Errorf("workspace/opensandbox: environment name %q is invalid", name)
		}
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("workspace/opensandbox: environment value contains NUL byte")
		}
		if config.env == nil {
			config.env = map[string]string{}
		}
		config.env[name] = value
		return nil
	}
}

// WithSandboxMetadata merges metadata used when creating a new Sandbox.
// The Workspace always overwrites the reserved agentscope.workspace.id key.
func WithSandboxMetadata(metadata map[string]string) Option {
	return func(config *config) error {
		if config.metadata == nil {
			config.metadata = map[string]string{}
		}
		for key, value := range metadata {
			key = strings.TrimSpace(key)
			if key == "" || strings.ContainsRune(key, '\x00') || strings.ContainsRune(value, '\x00') {
				return fmt.Errorf("workspace/opensandbox: invalid sandbox metadata")
			}
			config.metadata[key] = value
		}
		return nil
	}
}

// WithResourceLimits sets resource limits used when creating a new Sandbox.
func WithResourceLimits(limits ResourceLimits) Option {
	return func(config *config) error {
		config.resourceLimits = cloneResourceLimits(limits)
		return nil
	}
}

// WithEntrypoint sets the entrypoint argv used when creating a new Sandbox.
func WithEntrypoint(entrypoint ...string) Option {
	return func(config *config) error {
		for _, argument := range entrypoint {
			if strings.ContainsRune(argument, '\x00') {
				return fmt.Errorf("workspace/opensandbox: entrypoint contains NUL byte")
			}
		}
		config.entrypoint = append([]string(nil), entrypoint...)
		return nil
	}
}

// WithNetworkPolicy sets the network policy used when creating a new Sandbox.
func WithNetworkPolicy(policy *NetworkPolicy) Option {
	return func(config *config) error {
		config.networkPolicy = cloneNetworkPolicy(policy)
		return nil
	}
}

// WithExtraPythonPackages adds Python requirements installed during the first gateway bootstrap.
func WithExtraPythonPackages(requirements ...string) Option {
	return func(config *config) error {
		for _, requirement := range requirements {
			requirement = strings.TrimSpace(requirement)
			if requirement == "" || strings.HasPrefix(requirement, "-") || strings.ContainsRune(requirement, '\x00') {
				return fmt.Errorf("workspace/opensandbox: invalid Python requirement %q", requirement)
			}
			config.extraPythonPackages = append(config.extraPythonPackages, requirement)
		}
		return nil
	}
}

// WithInstructions sets the Workspace system prompt template. {workdir} is replaced at runtime.
func WithInstructions(instructions string) Option {
	return func(config *config) error {
		if strings.TrimSpace(instructions) == "" {
			return fmt.Errorf("workspace/opensandbox: instructions are empty")
		}
		config.instructions = instructions
		return nil
	}
}

// WithMCPs sets MCP clients seeded when the remote `.mcp` file is first created.
func WithMCPs(clients ...workspace.MCPClient) Option {
	return func(config *config) error {
		for _, client := range clients {
			if client == nil {
				return fmt.Errorf("workspace/opensandbox: nil MCP client")
			}
		}
		config.defaultMCPs = append(config.defaultMCPs, clients...)
		return nil
	}
}

// WithSkillPaths sets local Skill directories seeded into the remote Workspace during Initialize.
func WithSkillPaths(paths ...string) Option {
	return func(config *config) error {
		for _, skillPath := range paths {
			if strings.TrimSpace(skillPath) == "" {
				return fmt.Errorf("workspace/opensandbox: skill path is empty")
			}
		}
		config.skillPaths = append(config.skillPaths, paths...)
		return nil
	}
}

func withRuntime(runtime sandboxRuntime) Option {
	return func(config *config) error {
		if runtime == nil {
			return fmt.Errorf("workspace/opensandbox: nil runtime")
		}
		config.runtime = runtime
		return nil
	}
}

func cloneResourceLimits(values sdk.ResourceLimits) sdk.ResourceLimits {
	if len(values) == 0 {
		return nil
	}
	out := make(sdk.ResourceLimits, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneNetworkPolicy(policy *sdk.NetworkPolicy) *sdk.NetworkPolicy {
	if policy == nil {
		return nil
	}
	out := *policy
	out.Egress = append([]sdk.NetworkRule(nil), policy.Egress...)
	return &out
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
