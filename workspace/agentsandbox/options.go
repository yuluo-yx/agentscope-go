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

package agentsandbox

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

var shellEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Option configures an Agent Sandbox workspace.
type Option func(*Workspace) error

// WithWorkspaceID sets a stable workspace ID.
func WithWorkspaceID(id string) Option {
	return func(workspace *Workspace) error {
		workspace.id = strings.TrimSpace(id)
		return nil
	}
}

// WithTemplateName sets the agent-sandbox SandboxTemplate name.
func WithTemplateName(name string) Option {
	return func(workspace *Workspace) error {
		workspace.templateName = strings.TrimSpace(name)
		return nil
	}
}

// WithNamespace sets the Kubernetes namespace that contains the SandboxClaim.
func WithNamespace(namespace string) Option {
	return func(workspace *Workspace) error {
		workspace.namespace = strings.TrimSpace(namespace)
		return nil
	}
}

// WithContainerWorkdir sets the tool execution working directory inside the sandbox.
func WithContainerWorkdir(workdir string) Option {
	return func(workspace *Workspace) error {
		workdir = strings.TrimSpace(workdir)
		if workdir == "" {
			return fmt.Errorf("workspace/agentsandbox: container workdir is empty")
		}
		if !strings.HasPrefix(workdir, "/") {
			return fmt.Errorf("workspace/agentsandbox: container workdir must be absolute")
		}
		workspace.containerWorkdir = cleanSandboxPath(workdir)
		return nil
	}
}

// WithHostWorkdir sets the host mirror directory for offload, skills, and MCP indexes.
func WithHostWorkdir(workdir string) Option {
	return func(workspace *Workspace) error {
		workdir = strings.TrimSpace(workdir)
		if workdir == "" {
			return fmt.Errorf("workspace/agentsandbox: host workdir is empty")
		}
		abs, err := filepath.Abs(workdir)
		if err != nil {
			return err
		}
		workspace.hostWorkdir = filepath.Clean(abs)
		return nil
	}
}

// WithInstructions sets the workspace system prompt template.
func WithInstructions(instructions string) Option {
	return func(workspace *Workspace) error {
		workspace.instructions = instructions
		return nil
	}
}

// WithKeepSandbox keeps the SandboxClaim on Close and only disconnects locally.
func WithKeepSandbox(keep bool) Option {
	return func(workspace *Workspace) error {
		workspace.keepSandbox = keep
		return nil
	}
}

// WithPortForward uses the SDK port-forward connection mode.
func WithPortForward() Option {
	return func(workspace *Workspace) error {
		workspace.mode = connectionModePortForward
		workspace.apiURL = ""
		workspace.gatewayName = ""
		workspace.gatewayNamespace = ""
		return nil
	}
}

// WithAPIURL uses the sandbox-router direct URL connection mode.
func WithAPIURL(apiURL string) Option {
	return func(workspace *Workspace) error {
		apiURL = strings.TrimSpace(apiURL)
		if apiURL == "" {
			return fmt.Errorf("workspace/agentsandbox: API URL is empty")
		}
		workspace.apiURL = apiURL
		workspace.mode = connectionModeDirectURL
		return nil
	}
}

// WithGateway uses the Kubernetes Gateway API connection mode.
func WithGateway(name, namespace string) Option {
	return func(workspace *Workspace) error {
		name = strings.TrimSpace(name)
		namespace = strings.TrimSpace(namespace)
		if name == "" {
			return fmt.Errorf("workspace/agentsandbox: gateway name is empty")
		}
		if namespace == "" {
			return fmt.Errorf("workspace/agentsandbox: gateway namespace is empty")
		}
		workspace.gatewayName = name
		workspace.gatewayNamespace = namespace
		workspace.mode = connectionModeGateway
		return nil
	}
}

// WithServerPort sets the sandbox runtime service port.
func WithServerPort(port int) Option {
	return func(workspace *Workspace) error {
		if port < 0 {
			return fmt.Errorf("workspace/agentsandbox: server port must be non-negative")
		}
		workspace.serverPort = port
		return nil
	}
}

// WithEnv sets an environment variable for sandbox tool commands.
func WithEnv(name, value string) Option {
	return func(workspace *Workspace) error {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("workspace/agentsandbox: env name is empty")
		}
		if !shellEnvNamePattern.MatchString(name) {
			return fmt.Errorf("workspace/agentsandbox: env name %q is not a valid shell identifier", name)
		}
		workspace.env[name] = value
		return nil
	}
}

// WithMCPs sets the MCP clients registered during workspace initialization.
func WithMCPs(mcps ...asworkspace.MCPClient) Option {
	return func(workspace *Workspace) error {
		workspace.defaultMCPs = append([]asworkspace.MCPClient(nil), mcps...)
		return nil
	}
}

// WithRequestTimeout sets the timeout for one SDK request.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(workspace *Workspace) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/agentsandbox: request timeout must be positive")
		}
		workspace.requestTimeout = timeout
		return nil
	}
}

// WithOpenTimeout sets the timeout for sandbox creation and connection.
func WithOpenTimeout(timeout time.Duration) Option {
	return func(workspace *Workspace) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/agentsandbox: open timeout must be positive")
		}
		workspace.openTimeout = timeout
		return nil
	}
}

// WithMaxUploadSize sets the maximum file upload size in bytes.
func WithMaxUploadSize(bytes int64) Option {
	return func(workspace *Workspace) error {
		if bytes < 0 {
			return fmt.Errorf("workspace/agentsandbox: max upload size must be non-negative")
		}
		workspace.maxUploadSize = bytes
		return nil
	}
}

// WithMaxDownloadSize sets the maximum file download size in bytes.
func WithMaxDownloadSize(bytes int64) Option {
	return func(workspace *Workspace) error {
		if bytes < 0 {
			return fmt.Errorf("workspace/agentsandbox: max download size must be non-negative")
		}
		workspace.maxDownloadSize = bytes
		return nil
	}
}

func withRuntime(rt sandboxRuntime) Option {
	return func(workspace *Workspace) error {
		if rt == nil {
			return fmt.Errorf("workspace/agentsandbox: runtime is nil")
		}
		workspace.runtime = rt
		workspace.ownsRuntime = true
		return nil
	}
}
