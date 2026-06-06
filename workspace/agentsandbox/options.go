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

// Option 配置 Agent Sandbox workspace。
type Option func(*Workspace) error

// WithWorkspaceID 设置稳定 workspace ID。
func WithWorkspaceID(id string) Option {
	return func(workspace *Workspace) error {
		workspace.id = strings.TrimSpace(id)
		return nil
	}
}

// WithTemplateName 设置 agent-sandbox SandboxTemplate 名称。
func WithTemplateName(name string) Option {
	return func(workspace *Workspace) error {
		workspace.templateName = strings.TrimSpace(name)
		return nil
	}
}

// WithNamespace 设置 SandboxClaim 所在 Kubernetes namespace。
func WithNamespace(namespace string) Option {
	return func(workspace *Workspace) error {
		workspace.namespace = strings.TrimSpace(namespace)
		return nil
	}
}

// WithContainerWorkdir 设置 sandbox 内工具执行工作目录。
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

// WithHostWorkdir 设置宿主 mirror 目录，用于 offload、skills 和 MCP 索引。
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

// WithInstructions 设置 workspace system prompt 模板。
func WithInstructions(instructions string) Option {
	return func(workspace *Workspace) error {
		workspace.instructions = instructions
		return nil
	}
}

// WithKeepSandbox 在 Close 时保留 SandboxClaim，只断开本地连接。
func WithKeepSandbox(keep bool) Option {
	return func(workspace *Workspace) error {
		workspace.keepSandbox = keep
		return nil
	}
}

// WithPortForward 使用 SDK 的 port-forward 连接模式。
func WithPortForward() Option {
	return func(workspace *Workspace) error {
		workspace.mode = connectionModePortForward
		workspace.apiURL = ""
		workspace.gatewayName = ""
		workspace.gatewayNamespace = ""
		return nil
	}
}

// WithAPIURL 使用 sandbox-router direct URL 连接模式。
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

// WithGateway 使用 Kubernetes Gateway API 连接模式。
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

// WithServerPort 设置 sandbox runtime 服务端口。
func WithServerPort(port int) Option {
	return func(workspace *Workspace) error {
		if port < 0 {
			return fmt.Errorf("workspace/agentsandbox: server port must be non-negative")
		}
		workspace.serverPort = port
		return nil
	}
}

// WithEnv 设置 sandbox 工具命令环境变量。
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

// WithMCPs 设置 workspace 初始化时的 MCP clients。
func WithMCPs(mcps ...asworkspace.MCPClient) Option {
	return func(workspace *Workspace) error {
		workspace.defaultMCPs = append([]asworkspace.MCPClient(nil), mcps...)
		return nil
	}
}

// WithRequestTimeout 设置单次 SDK 请求超时。
func WithRequestTimeout(timeout time.Duration) Option {
	return func(workspace *Workspace) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/agentsandbox: request timeout must be positive")
		}
		workspace.requestTimeout = timeout
		return nil
	}
}

// WithOpenTimeout 设置 sandbox 创建和连接超时。
func WithOpenTimeout(timeout time.Duration) Option {
	return func(workspace *Workspace) error {
		if timeout <= 0 {
			return fmt.Errorf("workspace/agentsandbox: open timeout must be positive")
		}
		workspace.openTimeout = timeout
		return nil
	}
}

// WithMaxUploadSize 设置文件上传最大字节数。
func WithMaxUploadSize(bytes int64) Option {
	return func(workspace *Workspace) error {
		if bytes < 0 {
			return fmt.Errorf("workspace/agentsandbox: max upload size must be non-negative")
		}
		workspace.maxUploadSize = bytes
		return nil
	}
}

// WithMaxDownloadSize 设置文件下载最大字节数。
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
