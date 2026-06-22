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

package microsandbox

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

var shellEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Option configures a Microsandbox workspace.
type Option func(*Workspace) error

// WithWorkspaceID sets a stable AgentScope workspace ID.
func WithWorkspaceID(id string) Option {
	return func(workspace *Workspace) error {
		workspace.id = strings.TrimSpace(id)
		return nil
	}
}

// WithSandboxName sets the Microsandbox sandbox name.
func WithSandboxName(name string) Option {
	return func(workspace *Workspace) error {
		workspace.sandboxName = strings.TrimSpace(name)
		return nil
	}
}

// WithImage creates the sandbox from an OCI image.
func WithImage(image string) Option {
	return func(workspace *Workspace) error {
		image = strings.TrimSpace(image)
		if image == "" {
			return fmt.Errorf("workspace/microsandbox: image is empty")
		}
		workspace.image = image
		return nil
	}
}

// WithContainerWorkdir sets the tool execution directory inside Microsandbox.
func WithContainerWorkdir(workdir string) Option {
	return func(workspace *Workspace) error {
		workdir = strings.TrimSpace(workdir)
		if workdir == "" {
			return fmt.Errorf("workspace/microsandbox: container workdir is empty")
		}
		if !strings.HasPrefix(workdir, "/") {
			return fmt.Errorf("workspace/microsandbox: container workdir must be absolute")
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
			return fmt.Errorf("workspace/microsandbox: host workdir is empty")
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

// WithKeepSandbox keeps the Microsandbox running on Close and only detaches locally.
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
			return fmt.Errorf("workspace/microsandbox: env name is empty")
		}
		if !shellEnvNamePattern.MatchString(name) {
			return fmt.Errorf("workspace/microsandbox: env name %q is not a valid shell identifier", name)
		}
		workspace.env[name] = value
		return nil
	}
}

// WithCPUs sets the sandbox CPU count.
func WithCPUs(cpus uint8) Option {
	return func(workspace *Workspace) error {
		if cpus == 0 {
			return fmt.Errorf("workspace/microsandbox: CPUs must be positive")
		}
		workspace.cpus = cpus
		return nil
	}
}

// WithMemoryMiB sets the sandbox memory limit in MiB.
func WithMemoryMiB(memory uint32) Option {
	return func(workspace *Workspace) error {
		if memory == 0 {
			return fmt.Errorf("workspace/microsandbox: memory must be positive")
		}
		workspace.memoryMiB = memory
		return nil
	}
}

// WithEnsureInstalled controls whether Initialize downloads/loads the Microsandbox runtime.
func WithEnsureInstalled(ensure bool) Option {
	return func(workspace *Workspace) error {
		workspace.ensureInstalled = ensure
		return nil
	}
}

// WithRequestTimeout sets the timeout for one tool request.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(workspace *Workspace) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/microsandbox: request timeout must be positive")
		}
		workspace.requestTimeout = timeout
		return nil
	}
}

// WithOpenTimeout sets the timeout for sandbox creation.
func WithOpenTimeout(timeout time.Duration) Option {
	return func(workspace *Workspace) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/microsandbox: open timeout must be positive")
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
			return fmt.Errorf("workspace/microsandbox: runtime is nil")
		}
		workspace.runtime = rt
		workspace.ownsRuntime = true
		return nil
	}
}
