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

package daytona

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

var shellEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Option configures a Daytona workspace.
type Option func(*Workspace) error

// WithWorkspaceID sets a stable workspace ID.
func WithWorkspaceID(id string) Option {
	return func(workspace *Workspace) error {
		workspace.id = strings.TrimSpace(id)
		return nil
	}
}

// WithSandboxID connects to an existing Daytona sandbox by ID.
func WithSandboxID(id string) Option {
	return func(workspace *Workspace) error {
		workspace.sandboxID = strings.TrimSpace(id)
		return nil
	}
}

// WithSandboxName connects to an existing Daytona sandbox by name.
func WithSandboxName(name string) Option {
	return func(workspace *Workspace) error {
		workspace.sandboxName = strings.TrimSpace(name)
		return nil
	}
}

// WithImage creates new sandboxes from an OCI image.
func WithImage(image string) Option {
	return func(workspace *Workspace) error {
		image = strings.TrimSpace(image)
		if image == "" {
			return fmt.Errorf("workspace/daytona: image is empty")
		}
		workspace.image = image
		return nil
	}
}

// WithSnapshot creates new sandboxes from a Daytona snapshot.
func WithSnapshot(snapshot string) Option {
	return func(workspace *Workspace) error {
		snapshot = strings.TrimSpace(snapshot)
		if snapshot == "" {
			return fmt.Errorf("workspace/daytona: snapshot is empty")
		}
		workspace.snapshot = snapshot
		return nil
	}
}

// WithContainerWorkdir sets the tool execution working directory inside Daytona.
func WithContainerWorkdir(workdir string) Option {
	return func(workspace *Workspace) error {
		workdir = strings.TrimSpace(workdir)
		if workdir == "" {
			return fmt.Errorf("workspace/daytona: container workdir is empty")
		}
		if !strings.HasPrefix(workdir, "/") {
			return fmt.Errorf("workspace/daytona: container workdir must be absolute")
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
			return fmt.Errorf("workspace/daytona: host workdir is empty")
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

// WithKeepSandbox keeps the Daytona sandbox on Close and only disconnects locally.
func WithKeepSandbox(keep bool) Option {
	return func(workspace *Workspace) error {
		workspace.keepSandbox = keep
		return nil
	}
}

// WithEnv sets an environment variable for sandbox tool commands.
func WithEnv(name, value string) Option {
	return func(workspace *Workspace) error {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("workspace/daytona: env name is empty")
		}
		if !shellEnvNamePattern.MatchString(name) {
			return fmt.Errorf("workspace/daytona: env name %q is not a valid shell identifier", name)
		}
		workspace.env[name] = value
		return nil
	}
}

// WithAPIKey sets the Daytona API key.
func WithAPIKey(apiKey string) Option {
	return func(workspace *Workspace) error {
		workspace.apiKey = strings.TrimSpace(apiKey)
		return nil
	}
}

// WithJWTToken sets the Daytona JWT token.
func WithJWTToken(token string) Option {
	return func(workspace *Workspace) error {
		workspace.jwtToken = strings.TrimSpace(token)
		return nil
	}
}

// WithOrganizationID sets the Daytona organization ID used with JWT auth.
func WithOrganizationID(id string) Option {
	return func(workspace *Workspace) error {
		workspace.organizationID = strings.TrimSpace(id)
		return nil
	}
}

// WithAPIURL sets a custom Daytona API URL.
func WithAPIURL(apiURL string) Option {
	return func(workspace *Workspace) error {
		apiURL = strings.TrimSpace(apiURL)
		if apiURL == "" {
			return fmt.Errorf("workspace/daytona: API URL is empty")
		}
		workspace.apiURL = apiURL
		return nil
	}
}

// WithTarget sets the Daytona target/region.
func WithTarget(target string) Option {
	return func(workspace *Workspace) error {
		workspace.target = strings.TrimSpace(target)
		return nil
	}
}

// WithResources sets CPU, memory, and disk resources for newly created sandboxes.
func WithResources(cpu, memory, disk int) Option {
	return func(workspace *Workspace) error {
		if cpu < 0 || memory < 0 || disk < 0 {
			return fmt.Errorf("workspace/daytona: resources must be non-negative")
		}
		workspace.cpu = cpu
		workspace.memory = memory
		workspace.disk = disk
		return nil
	}
}

// WithGPU sets the GPU count for newly created sandboxes.
func WithGPU(gpu int) Option {
	return func(workspace *Workspace) error {
		if gpu < 0 {
			return fmt.Errorf("workspace/daytona: GPU count must be non-negative")
		}
		workspace.gpu = gpu
		return nil
	}
}

// WithRequestTimeout sets the timeout for one Daytona toolbox request.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(workspace *Workspace) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/daytona: request timeout must be positive")
		}
		workspace.requestTimeout = timeout
		return nil
	}
}

// WithOpenTimeout sets the timeout for Daytona sandbox creation and connection.
func WithOpenTimeout(timeout time.Duration) Option {
	return func(workspace *Workspace) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/daytona: open timeout must be positive")
		}
		workspace.openTimeout = timeout
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

func withRuntime(rt sandboxRuntime) Option {
	return func(workspace *Workspace) error {
		if rt == nil {
			return fmt.Errorf("workspace/daytona: runtime is nil")
		}
		workspace.runtime = rt
		workspace.ownsRuntime = true
		return nil
	}
}
