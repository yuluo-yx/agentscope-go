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
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sdk "github.com/alibaba/OpenSandbox/sdks/sandbox/go"

	"github.com/yuluo-yx/agentscope-go/pkg/workspace/internal/sandboxed"
)

type healthReply struct {
	healthy bool
	err     error
}

type runCall struct {
	argv    []string
	cwd     string
	env     map[string]string
	timeout time.Duration
}

type fakeSandboxHandle struct {
	id                   string
	healthReplies        []healthReply
	healthCalls          int
	healthDeadlines      []time.Time
	waitForHealthContext bool
	onHealthy            func()
	runCalls             []runCall
	runResult            runResult
	runErr               error
	files                map[string][]byte
	readErr              error
	writeErr             error
	pauseErr             error
	closeErr             error
	events               []string
}

func (h *fakeSandboxHandle) ID() string { return h.id }

func (h *fakeSandboxHandle) Healthy(ctx context.Context) (bool, error) {
	h.healthCalls++
	if deadline, ok := ctx.Deadline(); ok {
		h.healthDeadlines = append(h.healthDeadlines, deadline)
	}
	if h.onHealthy != nil {
		h.onHealthy()
	}
	if h.waitForHealthContext {
		<-ctx.Done()
		return false, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if len(h.healthReplies) == 0 {
		return true, nil
	}
	index := min(h.healthCalls-1, len(h.healthReplies)-1)
	reply := h.healthReplies[index]
	return reply.healthy, reply.err
}

func (h *fakeSandboxHandle) Run(
	_ context.Context,
	argv []string,
	cwd string,
	env map[string]string,
	timeout time.Duration,
) (runResult, error) {
	h.runCalls = append(h.runCalls, runCall{
		argv:    append([]string(nil), argv...),
		cwd:     cwd,
		env:     cloneStringMap(env),
		timeout: timeout,
	})
	return h.runResult, h.runErr
}

func (h *fakeSandboxHandle) ReadFile(_ context.Context, filename string) ([]byte, error) {
	if h.readErr != nil {
		return nil, h.readErr
	}
	return append([]byte(nil), h.files[filename]...), nil
}

func (h *fakeSandboxHandle) WriteFile(_ context.Context, filename string, data []byte) error {
	if h.writeErr != nil {
		return h.writeErr
	}
	if h.files == nil {
		h.files = map[string][]byte{}
	}
	h.files[filename] = append([]byte(nil), data...)
	return nil
}

func (h *fakeSandboxHandle) Pause(context.Context) error {
	h.events = append(h.events, "pause")
	return h.pauseErr
}

func (h *fakeSandboxHandle) Close() error {
	h.events = append(h.events, "close")
	return h.closeErr
}

type fakeSandboxRuntime struct {
	infos             []sandboxInfo
	handle            sandboxHandle
	listErr           error
	createErr         error
	connectErr        error
	resumeErr         error
	calls             []string
	listedWorkspaceID string
	createdSpec       sandboxSpec
	selectedID        string
	selectedTimeout   time.Duration
}

func (r *fakeSandboxRuntime) List(
	_ context.Context,
	_ sdk.ConnectionConfig,
	workspaceID string,
) ([]sandboxInfo, error) {
	r.calls = append(r.calls, "list")
	r.listedWorkspaceID = workspaceID
	return append([]sandboxInfo(nil), r.infos...), r.listErr
}

func (r *fakeSandboxRuntime) Create(_ context.Context, spec sandboxSpec) (sandboxHandle, error) {
	r.calls = append(r.calls, "create")
	r.createdSpec = spec
	return r.handle, r.createErr
}

func (r *fakeSandboxRuntime) Connect(
	_ context.Context,
	_ sdk.ConnectionConfig,
	sandboxID string,
	timeout time.Duration,
) (sandboxHandle, error) {
	r.calls = append(r.calls, "connect")
	r.selectedID = sandboxID
	r.selectedTimeout = timeout
	return r.handle, r.connectErr
}

func (r *fakeSandboxRuntime) Resume(
	_ context.Context,
	_ sdk.ConnectionConfig,
	sandboxID string,
	timeout time.Duration,
) (sandboxHandle, error) {
	r.calls = append(r.calls, "resume")
	r.selectedID = sandboxID
	r.selectedTimeout = timeout
	return r.handle, r.resumeErr
}

func TestProviderSelectsCreateConnectOrResume(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name           string
		infos          []sandboxInfo
		expectedAction string
		expectedID     string
	}{
		{
			name:           "create when no reusable sandbox exists",
			expectedAction: "create",
		},
		{
			name: "connect newest running sandbox",
			infos: []sandboxInfo{
				{ID: "older-paused", State: sdk.StatePaused, CreatedAt: now.Add(-time.Hour)},
				{ID: "newest-running", State: sdk.StateRunning, CreatedAt: now},
			},
			expectedAction: "connect",
			expectedID:     "newest-running",
		},
		{
			name: "resume newest paused sandbox",
			infos: []sandboxInfo{
				{ID: "older-running", State: sdk.StateRunning, CreatedAt: now.Add(-time.Hour)},
				{ID: "newest-paused", State: sdk.StatePaused, CreatedAt: now},
			},
			expectedAction: "resume",
			expectedID:     "newest-paused",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handle := &fakeSandboxHandle{id: "sandbox"}
			runtime := &fakeSandboxRuntime{infos: tt.infos, handle: handle}
			provider := &provider{
				runtime: runtime,
				spec: sandboxSpec{
					ID:      "workspace-id",
					Image:   "python:3.11-slim",
					Timeout: 45 * time.Second,
				},
			}

			opened, err := provider.Open(context.Background())
			if err != nil {
				t.Fatalf("Open returned error: %v", err)
			}
			openedBackend, ok := opened.(*backend)
			if !ok || openedBackend.handle != handle || openedBackend.workdir != defaultWorkdir {
				t.Fatalf("Open returned unexpected backend: %#v", opened)
			}
			expectedCalls := []string{"list", tt.expectedAction}
			if !reflect.DeepEqual(runtime.calls, expectedCalls) {
				t.Fatalf("runtime calls = %#v, want %#v", runtime.calls, expectedCalls)
			}
			if runtime.listedWorkspaceID != "workspace-id" {
				t.Fatalf("listed workspace ID = %q", runtime.listedWorkspaceID)
			}
			if tt.expectedAction == "create" {
				if runtime.createdSpec.ID != "workspace-id" || runtime.createdSpec.Image != "python:3.11-slim" {
					t.Fatalf("created spec mismatch: %#v", runtime.createdSpec)
				}
			} else if runtime.selectedID != tt.expectedID || runtime.selectedTimeout != 45*time.Second {
				t.Fatalf("selected sandbox mismatch: id=%q timeout=%s", runtime.selectedID, runtime.selectedTimeout)
			}
		})
	}
}

func TestProviderCachesHandleAndRetriesHealth(t *testing.T) {
	healthErr := errors.New("health endpoint unavailable")
	handle := &fakeSandboxHandle{
		id: "sandbox",
		healthReplies: []healthReply{
			{err: healthErr},
			{},
			{healthy: true},
		},
	}
	runtime := &fakeSandboxRuntime{handle: handle}
	provider := &provider{runtime: runtime, spec: sandboxSpec{ID: "workspace-id"}}

	first, err := provider.Open(context.Background())
	if err != nil {
		t.Fatalf("first Open returned error: %v", err)
	}
	second, err := provider.Open(context.Background())
	if err != nil {
		t.Fatalf("second Open returned error: %v", err)
	}
	if first.(*backend).handle != second.(*backend).handle {
		t.Fatal("cached Open should reuse the same handle")
	}
	if handle.healthCalls != 4 {
		t.Fatalf("health calls = %d, want 4", handle.healthCalls)
	}
	if !reflect.DeepEqual(runtime.calls, []string{"list", "create"}) {
		t.Fatalf("runtime calls = %#v", runtime.calls)
	}
}

func TestProviderBoundsHealthChecksWithHardTimeout(t *testing.T) {
	started := time.Now()
	handle := &fakeSandboxHandle{
		id:            "sandbox",
		healthReplies: []healthReply{{healthy: true}},
	}

	if err := (&provider{}).waitHealthy(context.Background(), handle); err != nil {
		t.Fatalf("waitHealthy returned error: %v", err)
	}
	if len(handle.healthDeadlines) != 1 {
		t.Fatalf("health deadlines = %d, want 1", len(handle.healthDeadlines))
	}
	remaining := handle.healthDeadlines[0].Sub(started)
	if remaining < providerHealthTimeout-time.Second || remaining > providerHealthTimeout+time.Second {
		t.Fatalf("health deadline = %s from start, want about %s", remaining, providerHealthTimeout)
	}
}

func TestProviderHealthCancellationRollsBackHandle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	handle := &fakeSandboxHandle{
		id:                   "sandbox",
		waitForHealthContext: true,
		onHealthy:            cancel,
	}
	runtime := &fakeSandboxRuntime{handle: handle}
	provider := &provider{runtime: runtime, spec: sandboxSpec{ID: "workspace-id"}}

	_, err := provider.Open(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open error = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(handle.events, []string{"pause", "close"}) {
		t.Fatalf("rollback events = %#v", handle.events)
	}
	if provider.handle != nil {
		t.Fatal("failed Open should clear provider handle")
	}
	if handle.healthCalls != 1 {
		t.Fatalf("health calls = %d, want 1", handle.healthCalls)
	}
}

func TestProviderCloseRetainsHandleWhenPauseFailsAndCanRetry(t *testing.T) {
	pauseErr := errors.New("pause failed")
	handle := &fakeSandboxHandle{id: "sandbox", pauseErr: pauseErr}
	provider := &provider{handle: handle}

	err := provider.Close(context.Background())
	if !errors.Is(err, pauseErr) {
		t.Fatalf("Close error = %v, want pause error", err)
	}
	if !reflect.DeepEqual(handle.events, []string{"pause"}) {
		t.Fatalf("Close events = %#v", handle.events)
	}
	if provider.handle != handle || provider.SandboxID() != "sandbox" {
		t.Fatal("pause failure should retain the cached handle for retry")
	}

	handle.pauseErr = nil
	if err := provider.Close(context.Background()); err != nil {
		t.Fatalf("retry Close returned error: %v", err)
	}
	if !reflect.DeepEqual(handle.events, []string{"pause", "pause", "close"}) {
		t.Fatalf("retry Close events = %#v", handle.events)
	}
	if provider.handle != nil || provider.SandboxID() != "" {
		t.Fatal("successful retry should clear the cached handle")
	}
}

func TestProviderCloseClearsHandleWhenLocalCloseFails(t *testing.T) {
	closeErr := errors.New("close failed")
	handle := &fakeSandboxHandle{id: "sandbox", closeErr: closeErr}
	provider := &provider{handle: handle}

	err := provider.Close(context.Background())
	if !errors.Is(err, closeErr) {
		t.Fatalf("Close error = %v, want local close error", err)
	}
	if !reflect.DeepEqual(handle.events, []string{"pause", "close"}) {
		t.Fatalf("Close events = %#v", handle.events)
	}
	if provider.handle != nil || provider.SandboxID() != "" {
		t.Fatal("successful pause should clear the handle even when local Close fails")
	}
	if err := provider.Close(context.Background()); err != nil {
		t.Fatalf("second Close should be idempotent: %v", err)
	}
}

func TestProviderRejectsRuntimeFailures(t *testing.T) {
	tests := []struct {
		name     string
		provider *provider
		contains string
	}{
		{name: "nil runtime", provider: &provider{}, contains: "nil provider runtime"},
		{
			name: "list failure",
			provider: &provider{
				runtime: &fakeSandboxRuntime{listErr: errors.New("list failed")},
				spec:    sandboxSpec{ID: "workspace-id"},
			},
			contains: "list sandboxes",
		},
		{
			name: "nil handle",
			provider: &provider{
				runtime: &fakeSandboxRuntime{},
				spec:    sandboxSpec{ID: "workspace-id"},
			},
			contains: "nil sandbox",
		},
		{
			name: "non attachable latest state",
			provider: &provider{
				runtime: &fakeSandboxRuntime{infos: []sandboxInfo{{ID: "failed", State: sdk.StateFailed}}},
				spec:    sandboxSpec{ID: "workspace-id"},
			},
			contains: "non-attachable state",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.provider.Open(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("Open error = %v, want substring %q", err, tt.contains)
			}
		})
	}
}

func TestBackendPreservesExecutionContract(t *testing.T) {
	handle := &fakeSandboxHandle{
		runResult: runResult{ExitCode: 7, Stdout: []byte("out"), Stderr: []byte("err")},
	}
	backend := &backend{handle: handle, workdir: defaultWorkdir}
	env := map[string]string{"TOKEN": "value"}
	argv := []string{"python3", "-c", "print('hello world')"}

	result, err := backend.Exec(context.Background(), argv, sandboxed.ExecOptions{
		Env:     env,
		Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if len(handle.runCalls) != 1 {
		t.Fatalf("Run calls = %d, want 1", len(handle.runCalls))
	}
	call := handle.runCalls[0]
	if !reflect.DeepEqual(call.argv, argv) || call.cwd != defaultWorkdir ||
		!reflect.DeepEqual(call.env, env) || call.timeout != 3*time.Second {
		t.Fatalf("Run call mismatch: %#v", call)
	}
	env["TOKEN"] = "changed"
	argv[0] = "changed"
	if call.env["TOKEN"] != "value" || call.argv[0] != "python3" {
		t.Fatal("Exec should copy argv and environment across the backend boundary")
	}
	handle.runResult.Stdout[0] = 'X'
	handle.runResult.Stderr[0] = 'Y'
	if result.ExitCode != 7 || string(result.Stdout) != "out" || string(result.Stderr) != "err" {
		t.Fatalf("Exec result mismatch: %#v", result)
	}

	_, err = backend.Exec(context.Background(), []string{"pwd"}, sandboxed.ExecOptions{CWD: "/tmp"})
	if err != nil || handle.runCalls[1].cwd != "/tmp" {
		t.Fatalf("explicit cwd was not preserved: call=%#v err=%v", handle.runCalls[1], err)
	}
	runErr := errors.New("run failed")
	handle.runErr = runErr
	if _, err := backend.Exec(context.Background(), []string{"false"}, sandboxed.ExecOptions{}); !errors.Is(err, runErr) {
		t.Fatalf("Exec error = %v, want %v", err, runErr)
	}
}

func TestBackendValidatesAndDelegatesFiles(t *testing.T) {
	tests := []struct {
		name    string
		backend *backend
		argv    []string
		cwd     string
	}{
		{name: "nil backend", backend: nil, argv: []string{"true"}},
		{name: "nil handle", backend: &backend{}, argv: []string{"true"}},
		{name: "empty argv", backend: &backend{handle: &fakeSandboxHandle{}}, argv: nil},
		{name: "blank command", backend: &backend{handle: &fakeSandboxHandle{}}, argv: []string{" "}},
		{name: "nul argument", backend: &backend{handle: &fakeSandboxHandle{}}, argv: []string{"echo", "bad\x00arg"}},
		{name: "relative cwd", backend: &backend{handle: &fakeSandboxHandle{}}, argv: []string{"pwd"}, cwd: "tmp"},
		{name: "nul cwd", backend: &backend{handle: &fakeSandboxHandle{}}, argv: []string{"pwd"}, cwd: "/tmp\x00bad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.backend.Exec(context.Background(), tt.argv, sandboxed.ExecOptions{CWD: tt.cwd})
			if err == nil {
				t.Fatal("Exec should return an error")
			}
		})
	}

	handle := &fakeSandboxHandle{files: map[string][]byte{"/workspace/input": []byte("payload")}}
	backendInstance := &backend{handle: handle}
	data, err := backendInstance.ReadFile(context.Background(), "/workspace/input")
	if err != nil || string(data) != "payload" {
		t.Fatalf("ReadFile result = %q, %v", data, err)
	}
	data, err = backendInstance.ReadFileLimit(context.Background(), "/workspace/input", int64(len("payload")))
	if err != nil || string(data) != "payload" {
		t.Fatalf("fallback ReadFileLimit result = %q, %v", data, err)
	}
	if _, err := backendInstance.ReadFileLimit(context.Background(), "/workspace/input", int64(len("payload")-1)); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("fallback ReadFileLimit over-limit error = %v", err)
	}
	if err := backendInstance.WriteFile(context.Background(), "/workspace/output", []byte("written")); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if string(handle.files["/workspace/output"]) != "written" {
		t.Fatalf("written file mismatch: %q", handle.files["/workspace/output"])
	}
	if _, err := (*backend)(nil).ReadFile(context.Background(), "/file"); err == nil {
		t.Fatal("nil ReadFile should return an error")
	}
	if _, err := (*backend)(nil).ReadFileLimit(context.Background(), "/file", 1); err == nil {
		t.Fatal("nil ReadFileLimit should return an error")
	}
	if err := (*backend)(nil).WriteFile(context.Background(), "/file", nil); err == nil {
		t.Fatal("nil WriteFile should return an error")
	}
	readErr := errors.New("read failed")
	writeErr := errors.New("write failed")
	handle.readErr = readErr
	handle.writeErr = writeErr
	if _, err := backendInstance.ReadFile(context.Background(), "/file"); !errors.Is(err, readErr) {
		t.Fatalf("ReadFile error = %v, want %v", err, readErr)
	}
	if _, err := backendInstance.ReadFileLimit(context.Background(), "/file", 1); !errors.Is(err, readErr) {
		t.Fatalf("fallback ReadFileLimit error = %v, want %v", err, readErr)
	}
	if err := backendInstance.WriteFile(context.Background(), "/file", nil); !errors.Is(err, writeErr) {
		t.Fatalf("WriteFile error = %v, want %v", err, writeErr)
	}
}
