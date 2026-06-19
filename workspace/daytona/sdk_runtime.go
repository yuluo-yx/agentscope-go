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
	"context"
	"fmt"
	"strings"
	"time"

	daytonasdk "github.com/daytonaio/daytona/libs/sdk-go/pkg/daytona"
	sdkoptions "github.com/daytonaio/daytona/libs/sdk-go/pkg/options"
	sdktypes "github.com/daytonaio/daytona/libs/sdk-go/pkg/types"
)

type sdkRuntime struct {
	client *daytonasdk.Client
}

func newSDKRuntime() (sandboxRuntime, error) {
	return &sdkRuntime{}, nil
}

func (r *sdkRuntime) Create(ctx context.Context, spec sandboxSpec) (sandboxHandle, error) {
	if r == nil {
		return nil, fmt.Errorf("workspace/daytona: nil SDK runtime")
	}
	client, err := r.clientForSpec(spec)
	if err != nil {
		return nil, err
	}
	params := createParamsFromSpec(spec)
	timeout := spec.OpenTimeout
	if timeout <= 0 {
		timeout = defaultOpenTimeout
	}
	sandbox, err := client.Create(ctx, params, sdkoptions.WithTimeout(timeout))
	if err != nil {
		return nil, fmt.Errorf("workspace/daytona: create sandbox: %w", err)
	}
	handle := &sdkHandle{sandbox: sandbox, spec: spec}
	if err := handle.ensureWorkdir(ctx); err != nil {
		_ = sandbox.Delete(context.Background())
		return nil, err
	}
	return handle, nil
}

func (r *sdkRuntime) Get(ctx context.Context, spec sandboxSpec, sandboxIDOrName string) (sandboxHandle, error) {
	if r == nil {
		return nil, fmt.Errorf("workspace/daytona: nil SDK runtime")
	}
	client, err := r.clientForSpec(spec)
	if err != nil {
		return nil, err
	}
	sandbox, err := client.Get(ctx, sandboxIDOrName)
	if err != nil {
		return nil, fmt.Errorf("workspace/daytona: get sandbox %q: %w", sandboxIDOrName, err)
	}
	if spec.Workdir == "" {
		spec.Workdir = defaultContainerWorkdir
	}
	if spec.RequestTimeout <= 0 {
		spec.RequestTimeout = defaultRequestTimeout
	}
	if spec.OpenTimeout <= 0 {
		spec.OpenTimeout = defaultOpenTimeout
	}
	return &sdkHandle{sandbox: sandbox, spec: spec}, nil
}

func (r *sdkRuntime) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	err := r.client.Close(context.Background())
	r.client = nil
	return err
}

func (r *sdkRuntime) clientForSpec(spec sandboxSpec) (*daytonasdk.Client, error) {
	if r.client != nil {
		return r.client, nil
	}
	client, err := daytonasdk.NewClientWithConfig(&sdktypes.DaytonaConfig{
		APIKey:         spec.APIKey,
		JWTToken:       spec.JWTToken,
		OrganizationID: spec.OrganizationID,
		APIUrl:         spec.APIURL,
		Target:         spec.Target,
		OtelEnabled:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("workspace/daytona: create SDK client: %w", err)
	}
	r.client = client
	return client, nil
}

func createParamsFromSpec(spec sandboxSpec) any {
	base := sdktypes.SandboxBaseParams{
		Name:     spec.ID,
		Language: sdktypes.CodeLanguagePython,
		EnvVars:  cloneStringMap(spec.Env),
		Labels: map[string]string{
			"agentscope-workspace-id": spec.ID,
		},
	}
	if spec.Snapshot != "" {
		return sdktypes.SnapshotParams{
			SandboxBaseParams: base,
			Snapshot:          spec.Snapshot,
		}
	}
	image := spec.Image
	if image == "" {
		image = defaultImage
	}
	resources := resourcesFromSpec(spec)
	return sdktypes.ImageParams{
		SandboxBaseParams: base,
		Image:             image,
		Resources:         resources,
	}
}

func resourcesFromSpec(spec sandboxSpec) *sdktypes.Resources {
	if spec.CPU == 0 && spec.GPU == 0 && spec.Memory == 0 && spec.Disk == 0 {
		return nil
	}
	return &sdktypes.Resources{
		CPU:    spec.CPU,
		GPU:    spec.GPU,
		Memory: spec.Memory,
		Disk:   spec.Disk,
	}
}

type sdkHandle struct {
	sandbox *daytonasdk.Sandbox
	spec    sandboxSpec
}

func (h *sdkHandle) ID() string {
	if h == nil || h.sandbox == nil {
		return ""
	}
	if h.sandbox.ID != "" {
		return h.sandbox.ID
	}
	return h.spec.ID
}

func (h *sdkHandle) IsReady(context.Context) (bool, error) {
	return h != nil && h.sandbox != nil, nil
}

func (h *sdkHandle) Run(ctx context.Context, req runRequest) (runResult, error) {
	if h == nil || h.sandbox == nil || h.sandbox.Process == nil {
		return runResult{}, fmt.Errorf("workspace/daytona: nil SDK sandbox handle")
	}
	workdir := req.Workdir
	if workdir == "" {
		workdir = h.workdir()
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = h.requestTimeout()
	}
	result, err := h.sandbox.Process.ExecuteCommand(ctx, req.Command,
		sdkoptions.WithCwd(workdir),
		sdkoptions.WithCommandEnv(cloneStringMap(req.Env)),
		sdkoptions.WithExecuteTimeout(timeout),
	)
	if err != nil {
		return runResult{}, err
	}
	if result == nil {
		return runResult{}, nil
	}
	return runResult{Stdout: result.Result, ExitCode: result.ExitCode}, nil
}

func (h *sdkHandle) Read(ctx context.Context, path string) ([]byte, error) {
	if h == nil || h.sandbox == nil || h.sandbox.FileSystem == nil {
		return nil, fmt.Errorf("workspace/daytona: nil SDK sandbox handle")
	}
	return h.sandbox.FileSystem.DownloadFile(ctx, path, nil)
}

func (h *sdkHandle) Write(ctx context.Context, targetPath string, data []byte) error {
	if h == nil || h.sandbox == nil || h.sandbox.FileSystem == nil {
		return fmt.Errorf("workspace/daytona: nil SDK sandbox handle")
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
			return fmt.Errorf("workspace/daytona: create parent directory %s failed with exit code %d: %s", dir, result.ExitCode, strings.TrimSpace(result.Stdout+result.Stderr))
		}
	}
	return h.sandbox.FileSystem.UploadFile(ctx, data, targetPath)
}

func (h *sdkHandle) Delete(ctx context.Context) error {
	if h == nil || h.sandbox == nil {
		return nil
	}
	timeout := h.spec.OpenTimeout
	if timeout <= 0 {
		timeout = defaultOpenTimeout
	}
	return h.sandbox.DeleteWithTimeout(ctx, timeout)
}

func (h *sdkHandle) Disconnect(context.Context) error {
	return nil
}

func (h *sdkHandle) ensureWorkdir(ctx context.Context) error {
	if h == nil || h.sandbox == nil {
		return fmt.Errorf("workspace/daytona: nil SDK sandbox handle")
	}
	result, err := h.Run(ctx, runRequest{
		Command: "mkdir -p " + shellQuote(h.workdir()),
		Workdir: "/",
		Timeout: h.requestTimeout(),
	})
	if err != nil {
		return fmt.Errorf("workspace/daytona: prepare sandbox workdir: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("workspace/daytona: prepare sandbox workdir failed with exit code %d: %s", result.ExitCode, strings.TrimSpace(result.Stdout+result.Stderr))
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

func filepathDir(path string) string {
	index := strings.LastIndex(path, "/")
	if index <= 0 {
		return "/"
	}
	return path[:index]
}
