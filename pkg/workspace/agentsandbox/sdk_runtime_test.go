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
	"errors"
	"strings"
	"testing"
	"time"

	agentsandboxsdk "sigs.k8s.io/agent-sandbox/clients/go/sandbox"
)

func TestSDKRuntimeBuildsOptionsAndCreatesSandbox(t *testing.T) {
	t.Parallel()

	var gotOptions agentsandboxsdk.Options
	client := &fakeSDKClient{handle: &fakeSDKSandbox{ready: true}}
	rt := &sdkRuntime{
		newClient: func(_ context.Context, opts agentsandboxsdk.Options) (sdkClient, error) {
			gotOptions = opts
			return client, nil
		},
	}

	handle, err := rt.Create(context.Background(), sandboxSpec{
		ID:               "workspace-1",
		TemplateName:     "python-sandbox-template",
		Namespace:        "agents",
		Workdir:          "/home/user/project",
		APIURL:           "http://sandbox-router.default.svc:8080",
		ServerPort:       9999,
		RequestTimeout:   30 * time.Second,
		OpenTimeout:      45 * time.Second,
		MaxUploadSize:    11,
		MaxDownloadSize:  22,
		GatewayName:      "ignored-when-api-url-is-set",
		GatewayNamespace: "agent-sandbox-system",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if handle == nil {
		t.Fatalf("Create should return handle")
	}
	if client.template != "python-sandbox-template" || client.namespace != "agents" {
		t.Fatalf("CreateSandbox arguments mismatch: template=%q namespace=%q", client.template, client.namespace)
	}
	if gotOptions.TemplateName != "python-sandbox-template" ||
		gotOptions.Namespace != "agents" ||
		gotOptions.APIURL != "http://sandbox-router.default.svc:8080" ||
		gotOptions.ServerPort != 9999 ||
		gotOptions.RequestTimeout != 30*time.Second ||
		gotOptions.SandboxReadyTimeout != 45*time.Second ||
		gotOptions.MaxUploadSize != 11 ||
		gotOptions.MaxDownloadSize != 22 ||
		!gotOptions.Quiet {
		t.Fatalf("SDK options mismatch: %#v", gotOptions)
	}
}

func TestSDKRuntimeWaitsForSandboxReachability(t *testing.T) {
	sandbox := &fakeSDKSandbox{
		ready:     true,
		runErrors: []error{errors.New("router returned 502")},
		runResult: &agentsandboxsdk.ExecutionResult{ExitCode: 0},
	}
	rt := &sdkRuntime{
		newClient: func(context.Context, agentsandboxsdk.Options) (sdkClient, error) {
			return &fakeSDKClient{handle: sandbox}, nil
		},
	}

	handle, err := rt.Create(context.Background(), sandboxSpec{
		TemplateName:   "python-sandbox-template",
		Namespace:      "default",
		OpenTimeout:    2 * time.Second,
		RequestTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Create should retry the readiness probe: %v", err)
	}
	if handle == nil {
		t.Fatalf("Create should return handle")
	}
	if sandbox.runCalls != 2 {
		t.Fatalf("expected two readiness probe calls, got %d", sandbox.runCalls)
	}
	if sandbox.closed {
		t.Fatalf("sandbox should not be closed after a successful retry")
	}
}

func TestSDKRuntimeHandlesFactoryCreateAndReachabilityErrors(t *testing.T) {
	t.Parallel()

	factoryErr := errors.New("factory failed")
	rt := &sdkRuntime{
		newClient: func(context.Context, agentsandboxsdk.Options) (sdkClient, error) {
			return nil, factoryErr
		},
	}
	if _, err := rt.Create(context.Background(), sandboxSpec{}); !errors.Is(err, factoryErr) {
		t.Fatalf("Create should return client factory error, got %v", err)
	}

	createErr := errors.New("create failed")
	rt = &sdkRuntime{
		newClient: func(context.Context, agentsandboxsdk.Options) (sdkClient, error) {
			return &fakeSDKClient{err: createErr}, nil
		},
	}
	if _, err := rt.Create(context.Background(), sandboxSpec{}); !errors.Is(err, createErr) {
		t.Fatalf("Create should return CreateSandbox error, got %v", err)
	}

	neverReady := &fakeSDKSandbox{}
	rt = &sdkRuntime{
		newClient: func(context.Context, agentsandboxsdk.Options) (sdkClient, error) {
			return &fakeSDKClient{handle: neverReady}, nil
		},
	}
	_, err := rt.Create(context.Background(), sandboxSpec{OpenTimeout: time.Nanosecond})
	if err == nil || !strings.Contains(err.Error(), "did not become reachable") {
		t.Fatalf("Create should time out waiting for readiness, got %v", err)
	}
	if !neverReady.closed {
		t.Fatalf("Create should close sandbox after reachability failure")
	}

	if err := waitForSDKSandboxReachable(context.Background(), nil, sandboxSpec{}); err == nil {
		t.Fatalf("waitForSDKSandboxReachable should reject nil sandbox")
	}
	if _, err := (*sdkRuntime)(nil).Create(context.Background(), sandboxSpec{}); err == nil {
		t.Fatalf("nil sdkRuntime should return error")
	}
	defaultRuntime, err := newSDKRuntime()
	if err != nil || defaultRuntime == nil {
		t.Fatalf("newSDKRuntime returned runtime=%#v err=%v", defaultRuntime, err)
	}
	if _, err := (*sdkClientAdapter)(nil).CreateSandbox(context.Background(), "template", "namespace"); err == nil {
		t.Fatalf("nil sdkClientAdapter should return error")
	}
	if _, err := (&sdkClientAdapter{}).CreateSandbox(context.Background(), "template", "namespace"); err == nil {
		t.Fatalf("empty sdkClientAdapter should return error")
	}
	portForwardOptions := sandboxOptionsFromSpec(sandboxSpec{
		Mode:             connectionModePortForward,
		APIURL:           "http://router",
		GatewayName:      "gateway",
		GatewayNamespace: "gateways",
	})
	if portForwardOptions.APIURL != "" || portForwardOptions.GatewayName != "" || portForwardOptions.GatewayNamespace != "" {
		t.Fatalf("port-forward options should clear direct routing settings: %#v", portForwardOptions)
	}
	if err := (&sdkRuntime{}).Close(); err != nil {
		t.Fatalf("Close should be a no-op: %v", err)
	}
}

func TestSDKHandleRunsWithWorkdirEnvAndTimeout(t *testing.T) {
	t.Parallel()

	sandbox := &fakeSDKSandbox{
		ready:     true,
		runResult: &agentsandboxsdk.ExecutionResult{Stdout: "ok\n", ExitCode: 0},
	}
	handle := &sdkHandle{sandbox: sandbox, spec: sandboxSpec{Workdir: "/home/user/project"}}
	result, err := handle.Run(context.Background(), runRequest{
		Command: "echo \"$FOO\"",
		Workdir: "/home/user/project",
		Env: map[string]string{
			"FOO": "bar baz",
		},
		Timeout: 1500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Stdout != "ok\n" || result.ExitCode != 0 {
		t.Fatalf("Run result mismatch: %#v", result)
	}
	want := "export FOO='bar baz'; cd '/home/user/project' && echo \"$FOO\""
	if sandbox.lastRun != want {
		t.Fatalf("wrapped command mismatch:\n got: %s\nwant: %s", sandbox.lastRun, want)
	}
	if len(sandbox.lastRunOptions) == 0 {
		t.Fatalf("Run should pass call options for timeout")
	}
}

func TestSDKHandleIDReadWriteCloseAndNilBranches(t *testing.T) {
	t.Parallel()

	handle := &sdkHandle{sandbox: &fakeSDKSandbox{
		ready:     true,
		claimName: "claim-1",
		runResult: &agentsandboxsdk.ExecutionResult{ExitCode: 0},
	}, spec: sandboxSpec{ID: "fallback-id", RequestTimeout: time.Second}}
	if got := handle.ID(); got != "claim-1" {
		t.Fatalf("ID should prefer claim name, got %q", got)
	}
	if ready, err := handle.IsReady(context.Background()); err != nil || !ready {
		t.Fatalf("IsReady mismatch: ready=%v err=%v", ready, err)
	}
	data, err := handle.Read(context.Background(), "/tmp/file.txt")
	if err != nil || string(data) != "read:/tmp/file.txt" {
		t.Fatalf("Read mismatch: data=%q err=%v", data, err)
	}
	if err := handle.Write(context.Background(), "plain.txt", []byte("plain")); err != nil {
		t.Fatalf("plain Write returned error: %v", err)
	}
	if len(handle.sandbox.(*fakeSDKSandbox).writes) != 1 || handle.sandbox.(*fakeSDKSandbox).writes[0].path != "plain.txt" {
		t.Fatalf("plain write should use target filename directly: %#v", handle.sandbox.(*fakeSDKSandbox).writes)
	}
	if err := handle.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := handle.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect returned error: %v", err)
	}
	sandbox := handle.sandbox.(*fakeSDKSandbox)
	if !sandbox.closed || !sandbox.disconnected {
		t.Fatalf("Close and Disconnect should forward to sandbox: closed=%v disconnected=%v", sandbox.closed, sandbox.disconnected)
	}

	nameFallback := &sdkHandle{sandbox: &fakeSDKSandbox{sandboxName: "sandbox-2"}, spec: sandboxSpec{ID: "fallback-id"}}
	if got := nameFallback.ID(); got != "sandbox-2" {
		t.Fatalf("ID should fall back to sandbox name, got %q", got)
	}
	idFallback := &sdkHandle{sandbox: &fakeSDKSandbox{}, spec: sandboxSpec{ID: "fallback-id"}}
	if got := idFallback.ID(); got != "fallback-id" {
		t.Fatalf("ID should fall back to spec ID, got %q", got)
	}
	if got := (*sdkHandle)(nil).ID(); got != "" {
		t.Fatalf("nil handle ID should be empty, got %q", got)
	}
	if ready, err := (*sdkHandle)(nil).IsReady(context.Background()); err != nil || ready {
		t.Fatalf("nil handle IsReady mismatch: ready=%v err=%v", ready, err)
	}
	if _, err := (*sdkHandle)(nil).Run(context.Background(), runRequest{}); err == nil {
		t.Fatalf("nil handle Run should return error")
	}
	if _, err := (*sdkHandle)(nil).Read(context.Background(), "x"); err == nil {
		t.Fatalf("nil handle Read should return error")
	}
	if err := (*sdkHandle)(nil).Write(context.Background(), "x", nil); err == nil {
		t.Fatalf("nil handle Write should return error")
	}
	if err := (*sdkHandle)(nil).Close(context.Background()); err != nil {
		t.Fatalf("nil handle Close should be nil, got %v", err)
	}
	if err := (*sdkHandle)(nil).Disconnect(context.Background()); err != nil {
		t.Fatalf("nil handle Disconnect should be nil, got %v", err)
	}
}

func TestSDKHandleWritesAbsolutePathThroughPlainUpload(t *testing.T) {
	t.Parallel()

	sandbox := &fakeSDKSandbox{
		ready:     true,
		runResult: &agentsandboxsdk.ExecutionResult{ExitCode: 0},
	}
	handle := &sdkHandle{sandbox: sandbox, spec: sandboxSpec{Workdir: "/home/user"}}
	if err := handle.Write(context.Background(), "/home/user/notes/todo.txt", []byte("hello")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if len(sandbox.writes) != 1 {
		t.Fatalf("expected one SDK upload, got %#v", sandbox.writes)
	}
	upload := sandbox.writes[0]
	if strings.Contains(upload.path, "/") || !strings.HasPrefix(upload.path, "agentscope-upload-") {
		t.Fatalf("SDK upload path must be a plain temp filename, got %q", upload.path)
	}
	if string(upload.content) != "hello" {
		t.Fatalf("upload content mismatch: %q", string(upload.content))
	}
	if !strings.Contains(sandbox.lastRun, "mkdir -p '/home/user/notes'") ||
		!strings.Contains(sandbox.lastRun, "cat '"+upload.path+"' > '/home/user/notes/todo.txt'") ||
		!strings.Contains(sandbox.lastRun, "rm -f '"+upload.path+"'") {
		t.Fatalf("staging command should move uploaded file into absolute target, got: %s", sandbox.lastRun)
	}
}

type fakeSDKClient struct {
	handle    *fakeSDKSandbox
	template  string
	namespace string
	err       error
}

func (c *fakeSDKClient) CreateSandbox(_ context.Context, template, namespace string) (sdkSandbox, error) {
	c.template = template
	c.namespace = namespace
	if c.err != nil {
		return nil, c.err
	}
	return c.handle, nil
}

type fakeSDKSandbox struct {
	ready          bool
	runResult      *agentsandboxsdk.ExecutionResult
	runErrors      []error
	runCalls       int
	lastRun        string
	lastRunOptions []agentsandboxsdk.CallOption
	writes         []fakeSDKWrite
	closed         bool
	disconnected   bool
	claimName      string
	sandboxName    string
}

type fakeSDKWrite struct {
	path    string
	content []byte
}

func (s *fakeSDKSandbox) IsReady() bool {
	return s.ready
}

func (s *fakeSDKSandbox) Run(_ context.Context, command string, opts ...agentsandboxsdk.CallOption) (*agentsandboxsdk.ExecutionResult, error) {
	s.runCalls++
	s.lastRun = command
	s.lastRunOptions = append([]agentsandboxsdk.CallOption(nil), opts...)
	if len(s.runErrors) > 0 {
		err := s.runErrors[0]
		s.runErrors = s.runErrors[1:]
		return nil, err
	}
	if s.runResult == nil {
		return &agentsandboxsdk.ExecutionResult{}, nil
	}
	return s.runResult, nil
}

func (s *fakeSDKSandbox) Read(_ context.Context, path string, _ ...agentsandboxsdk.CallOption) ([]byte, error) {
	return []byte("read:" + path), nil
}

func (s *fakeSDKSandbox) Write(_ context.Context, path string, content []byte, _ ...agentsandboxsdk.CallOption) error {
	s.writes = append(s.writes, fakeSDKWrite{path: path, content: append([]byte(nil), content...)})
	return nil
}

func (s *fakeSDKSandbox) Close(context.Context) error {
	s.closed = true
	return nil
}

func (s *fakeSDKSandbox) Disconnect(context.Context) error {
	s.disconnected = true
	return nil
}

func (s *fakeSDKSandbox) ClaimName() string {
	return s.claimName
}

func (s *fakeSDKSandbox) SandboxName() string {
	return s.sandboxName
}
