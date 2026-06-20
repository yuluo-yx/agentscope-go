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
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/superradcompany/microsandbox/sdk/go"
)

type sdkRuntime struct{}

func newSDKRuntime() (sandboxRuntime, error) {
	return &sdkRuntime{}, nil
}

func (r *sdkRuntime) Create(ctx context.Context, spec sandboxSpec) (sandboxHandle, error) {
	if r == nil {
		return nil, fmt.Errorf("workspace/microsandbox: nil SDK runtime")
	}
	openCtx, cancel := contextWithOptionalTimeout(ctx, spec.OpenTimeout)
	defer cancel()

	if spec.EnsureInstalled {
		if err := sdk.EnsureInstalled(openCtx); err != nil {
			return nil, fmt.Errorf("workspace/microsandbox: ensure runtime installed: %w", err)
		}
	}

	options := []sdk.SandboxOption{
		sdk.WithImage(imageFromSpec(spec)),
		sdk.WithWorkdir(workdirFromSpec(spec)),
	}
	if spec.CPUs > 0 {
		options = append(options, sdk.WithCPUs(spec.CPUs))
	}
	if spec.MemoryMiB > 0 {
		options = append(options, sdk.WithMemory(spec.MemoryMiB))
	}
	if len(spec.Env) > 0 {
		options = append(options, sdk.WithEnv(cloneStringMap(spec.Env)))
	}

	sandbox, err := sdk.CreateSandbox(openCtx, nameFromSpec(spec), options...)
	if err != nil {
		return nil, fmt.Errorf("workspace/microsandbox: create sandbox: %w", err)
	}
	handle := &sdkHandle{sandbox: sandbox, spec: spec}
	if err := handle.ensureWorkdir(ctx); err != nil {
		_ = sandbox.Stop(context.Background())
		_ = sandbox.Close()
		return nil, err
	}
	return handle, nil
}

func (r *sdkRuntime) Close() error {
	return nil
}

type sdkHandle struct {
	sandbox *sdk.Sandbox
	spec    sandboxSpec
}

func (h *sdkHandle) ID() string {
	if h == nil || h.sandbox == nil {
		return ""
	}
	return h.sandbox.Name()
}

func (h *sdkHandle) IsReady(context.Context) (bool, error) {
	return h != nil && h.sandbox != nil, nil
}

func (h *sdkHandle) Run(ctx context.Context, req runRequest) (runResult, error) {
	if h == nil || h.sandbox == nil {
		return runResult{}, fmt.Errorf("workspace/microsandbox: nil SDK sandbox handle")
	}
	workdir := req.Workdir
	if workdir == "" {
		workdir = h.workdir()
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = h.requestTimeout()
	}
	options := []sdk.ExecOption{
		sdk.WithExecCwd(workdir),
		sdk.WithExecTimeout(timeout),
	}
	if len(req.Env) > 0 {
		options = append(options, sdk.WithExecEnv(cloneStringMap(req.Env)))
	}
	output, err := h.sandbox.Shell(ctx, req.Command, options...)
	if err != nil {
		return runResult{}, err
	}
	if output == nil {
		return runResult{}, nil
	}
	return runResult{Stdout: output.Stdout(), Stderr: output.Stderr(), ExitCode: output.ExitCode()}, nil
}

func (h *sdkHandle) Read(ctx context.Context, path string) ([]byte, error) {
	if h == nil || h.sandbox == nil {
		return nil, fmt.Errorf("workspace/microsandbox: nil SDK sandbox handle")
	}
	return h.sandbox.FS().Read(ctx, path)
}

func (h *sdkHandle) Write(ctx context.Context, targetPath string, data []byte) error {
	if h == nil || h.sandbox == nil {
		return fmt.Errorf("workspace/microsandbox: nil SDK sandbox handle")
	}
	if dir := cleanSandboxPath(filepathDir(targetPath)); dir != "" && dir != "/" {
		result, err := h.Run(ctx, runRequest{
			Command: "mkdir -p " + shellQuote(dir),
			Workdir: h.workdir(),
			Timeout: h.requestTimeout(),
		})
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("workspace/microsandbox: create parent directory %s failed with exit code %d: %s", dir, result.ExitCode, strings.TrimSpace(result.Stdout+result.Stderr))
		}
	}
	return h.sandbox.FS().Write(ctx, targetPath, data)
}

func (h *sdkHandle) Stop(ctx context.Context) error {
	if h == nil || h.sandbox == nil {
		return nil
	}
	return h.sandbox.Stop(ctx)
}

func (h *sdkHandle) Detach(ctx context.Context) error {
	if h == nil || h.sandbox == nil {
		return nil
	}
	return h.sandbox.Detach(ctx)
}

func (h *sdkHandle) Close() error {
	if h == nil || h.sandbox == nil {
		return nil
	}
	return h.sandbox.Close()
}

func (h *sdkHandle) ensureWorkdir(ctx context.Context) error {
	if h == nil || h.sandbox == nil {
		return fmt.Errorf("workspace/microsandbox: nil SDK sandbox handle")
	}
	result, err := h.Run(ctx, runRequest{
		Command: "mkdir -p " + shellQuote(h.workdir()),
		Workdir: "/",
		Timeout: h.requestTimeout(),
	})
	if err != nil {
		return fmt.Errorf("workspace/microsandbox: prepare sandbox workdir: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("workspace/microsandbox: prepare sandbox workdir failed with exit code %d: %s", result.ExitCode, strings.TrimSpace(result.Stdout+result.Stderr))
	}
	return nil
}

func (h *sdkHandle) workdir() string {
	if h == nil || h.spec.Workdir == "" {
		return defaultContainerWorkdir
	}
	return h.spec.Workdir
}

func (h *sdkHandle) requestTimeout() time.Duration {
	if h == nil || h.spec.RequestTimeout <= 0 {
		return defaultRequestTimeout
	}
	return h.spec.RequestTimeout
}

func contextWithOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func nameFromSpec(spec sandboxSpec) string {
	if strings.TrimSpace(spec.Name) != "" {
		return strings.TrimSpace(spec.Name)
	}
	if strings.TrimSpace(spec.ID) != "" {
		return strings.TrimSpace(spec.ID)
	}
	return "agentscope-microsandbox"
}

func imageFromSpec(spec sandboxSpec) string {
	if spec.Image != "" {
		return spec.Image
	}
	return defaultImage
}

func workdirFromSpec(spec sandboxSpec) string {
	if spec.Workdir != "" {
		return spec.Workdir
	}
	return defaultContainerWorkdir
}

func filepathDir(path string) string {
	index := strings.LastIndex(path, "/")
	if index <= 0 {
		return "/"
	}
	return path[:index]
}
