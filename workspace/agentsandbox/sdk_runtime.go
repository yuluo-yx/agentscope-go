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
	"context"
	"fmt"
	pathpkg "path"
	"sort"
	"strings"
	"time"

	agentsandboxsdk "sigs.k8s.io/agent-sandbox/clients/go/sandbox"

	"github.com/yuluo-yx/agentscope-go/utils"
)

type sdkClient interface {
	CreateSandbox(context.Context, string, string) (sdkSandbox, error)
}

type sdkSandbox interface {
	IsReady() bool
	Run(context.Context, string, ...agentsandboxsdk.CallOption) (*agentsandboxsdk.ExecutionResult, error)
	Read(context.Context, string, ...agentsandboxsdk.CallOption) ([]byte, error)
	Write(context.Context, string, []byte, ...agentsandboxsdk.CallOption) error
	Close(context.Context) error
	Disconnect(context.Context) error
	ClaimName() string
	SandboxName() string
}

type sdkClientFactory func(context.Context, agentsandboxsdk.Options) (sdkClient, error)

type sdkRuntime struct {
	newClient sdkClientFactory
}

func newSDKRuntime() (sandboxRuntime, error) {
	return &sdkRuntime{newClient: defaultSDKClientFactory}, nil
}

func (r *sdkRuntime) Create(ctx context.Context, spec sandboxSpec) (sandboxHandle, error) {
	if r == nil {
		return nil, fmt.Errorf("workspace/agentsandbox: nil SDK runtime")
	}
	newClient := r.newClient
	if newClient == nil {
		newClient = defaultSDKClientFactory
	}
	client, err := newClient(ctx, sandboxOptionsFromSpec(spec))
	if err != nil {
		return nil, err
	}
	sandbox, err := client.CreateSandbox(ctx, spec.TemplateName, spec.Namespace)
	if err != nil {
		return nil, err
	}
	if err := waitForSDKSandboxReachable(ctx, sandbox, spec); err != nil {
		_ = sandbox.Close(context.Background())
		return nil, err
	}
	return &sdkHandle{sandbox: sandbox, spec: spec}, nil
}

func (r *sdkRuntime) Close() error {
	return nil
}

func defaultSDKClientFactory(ctx context.Context, opts agentsandboxsdk.Options) (sdkClient, error) {
	client, err := agentsandboxsdk.NewClient(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &sdkClientAdapter{client: client}, nil
}

func waitForSDKSandboxReachable(ctx context.Context, sandbox sdkSandbox, spec sandboxSpec) error {
	if sandbox == nil {
		return fmt.Errorf("workspace/agentsandbox: nil SDK sandbox")
	}
	timeout := spec.OpenTimeout
	if timeout <= 0 {
		timeout = defaultOpenTimeout
	}
	pollInterval := 500 * time.Millisecond
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		if sandbox.IsReady() {
			probeTimeout := minPositiveDuration(spec.RequestTimeout, 10*time.Second)
			result, err := sandbox.Run(waitCtx, "pwd >/dev/null", callOptions(probeTimeout)...)
			if err == nil && (result == nil || result.ExitCode == 0) {
				return nil
			}
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("probe command exited with code %d: %s%s", result.ExitCode, result.Stdout, result.Stderr)
			}
		} else {
			lastErr = fmt.Errorf("SDK sandbox is not connected")
		}

		select {
		case <-waitCtx.Done():
			if lastErr == nil {
				return fmt.Errorf("workspace/agentsandbox: sandbox did not become reachable within %s: %w", timeout, waitCtx.Err())
			}
			return fmt.Errorf("workspace/agentsandbox: sandbox did not become reachable within %s: %w", timeout, lastErr)
		case <-time.After(pollInterval):
		}
	}
}

type sdkClientAdapter struct {
	client *agentsandboxsdk.Client
}

func (c *sdkClientAdapter) CreateSandbox(ctx context.Context, template, namespace string) (sdkSandbox, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workspace/agentsandbox: nil SDK client")
	}
	return c.client.CreateSandbox(ctx, template, namespace)
}

func sandboxOptionsFromSpec(spec sandboxSpec) agentsandboxsdk.Options {
	opts := agentsandboxsdk.Options{
		TemplateName:        spec.TemplateName,
		Namespace:           spec.Namespace,
		APIURL:              spec.APIURL,
		GatewayName:         spec.GatewayName,
		GatewayNamespace:    spec.GatewayNamespace,
		ServerPort:          spec.ServerPort,
		SandboxReadyTimeout: spec.OpenTimeout,
		GatewayReadyTimeout: spec.OpenTimeout,
		RequestTimeout:      spec.RequestTimeout,
		MaxUploadSize:       spec.MaxUploadSize,
		MaxDownloadSize:     spec.MaxDownloadSize,
		Quiet:               true,
	}
	if spec.Mode == connectionModePortForward {
		opts.GatewayName = ""
		opts.GatewayNamespace = ""
		opts.APIURL = ""
	}
	if spec.APIURL != "" {
		opts.GatewayName = ""
		opts.GatewayNamespace = ""
	}
	return opts
}

type sdkHandle struct {
	sandbox sdkSandbox
	spec    sandboxSpec
}

func (h *sdkHandle) ID() string {
	if h == nil || h.sandbox == nil {
		return ""
	}
	if claim := h.sandbox.ClaimName(); claim != "" {
		return claim
	}
	if name := h.sandbox.SandboxName(); name != "" {
		return name
	}
	return h.spec.ID
}

func (h *sdkHandle) IsReady(context.Context) (bool, error) {
	if h == nil || h.sandbox == nil {
		return false, nil
	}
	return h.sandbox.IsReady(), nil
}

func (h *sdkHandle) Run(ctx context.Context, req runRequest) (runResult, error) {
	if h == nil || h.sandbox == nil {
		return runResult{}, fmt.Errorf("workspace/agentsandbox: nil SDK sandbox handle")
	}
	result, err := h.sandbox.Run(ctx, wrapRunCommand(req), callOptions(req.Timeout)...)
	if err != nil {
		return runResult{}, err
	}
	if result == nil {
		return runResult{}, nil
	}
	return runResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode}, nil
}

func (h *sdkHandle) Read(ctx context.Context, path string) ([]byte, error) {
	if h == nil || h.sandbox == nil {
		return nil, fmt.Errorf("workspace/agentsandbox: nil SDK sandbox handle")
	}
	return h.sandbox.Read(ctx, path, callOptions(h.spec.RequestTimeout)...)
}

func (h *sdkHandle) Write(ctx context.Context, targetPath string, data []byte) error {
	if h == nil || h.sandbox == nil {
		return fmt.Errorf("workspace/agentsandbox: nil SDK sandbox handle")
	}
	if isPlainSDKFilename(targetPath) {
		return h.sandbox.Write(ctx, targetPath, data, callOptions(h.spec.RequestTimeout)...)
	}
	tempName := "agentscope-upload-" + utils.NewID() + ".tmp"
	if err := h.sandbox.Write(ctx, tempName, data, callOptions(h.spec.RequestTimeout)...); err != nil {
		return err
	}
	dir := pathpkg.Dir(targetPath)
	command := fmt.Sprintf(
		"mkdir -p %s && cat %s > %s; status=$?; rm -f %s; exit $status",
		shellQuote(dir),
		shellQuote(tempName),
		shellQuote(targetPath),
		shellQuote(tempName),
	)
	result, err := h.sandbox.Run(ctx, command, callOptions(h.spec.RequestTimeout)...)
	if err != nil {
		return err
	}
	if result != nil && result.ExitCode != 0 {
		return fmt.Errorf("workspace/agentsandbox: staging write failed with exit code %d: %s%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	return nil
}

func (h *sdkHandle) Close(ctx context.Context) error {
	if h == nil || h.sandbox == nil {
		return nil
	}
	return h.sandbox.Close(ctx)
}

func (h *sdkHandle) Disconnect(ctx context.Context) error {
	if h == nil || h.sandbox == nil {
		return nil
	}
	return h.sandbox.Disconnect(ctx)
}

func wrapRunCommand(req runRequest) string {
	command := strings.TrimSpace(req.Command)
	prefix := make([]string, 0, len(req.Env)+1)
	if len(req.Env) > 0 {
		keys := make([]string, 0, len(req.Env))
		for key := range req.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if shellEnvNamePattern.MatchString(key) {
				prefix = append(prefix, fmt.Sprintf("export %s=%s", key, shellQuote(req.Env[key])))
			}
		}
	}
	if req.Workdir != "" {
		prefix = append(prefix, "cd "+shellQuote(req.Workdir))
	}
	if len(prefix) == 0 {
		return command
	}
	if len(prefix) == 1 && strings.HasPrefix(prefix[0], "cd ") {
		return prefix[0] + " && " + command
	}
	return strings.Join(prefix, "; ") + " && " + command
}

func callOptions(timeout time.Duration) []agentsandboxsdk.CallOption {
	if timeout <= 0 {
		return nil
	}
	return []agentsandboxsdk.CallOption{agentsandboxsdk.WithTimeout(timeout)}
}

func minPositiveDuration(value, limit time.Duration) time.Duration {
	if value <= 0 {
		return limit
	}
	if limit <= 0 || value < limit {
		return value
	}
	return limit
}

func isPlainSDKFilename(path string) bool {
	base := pathpkg.Base(path)
	return base != "." && base != ".." && base != "/" && base == path
}
