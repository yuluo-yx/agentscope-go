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

package sandboxed

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace/gateway"
)

func TestOperationGateWaitsRejectsAndReopens(t *testing.T) {
	t.Parallel()

	gate := &operationGate{}
	release, err := gate.begin(context.Background())
	if err != nil {
		t.Fatalf("begin returned error: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- gate.closeAndWait(context.Background()) }()
	waitForGateClosing(t, gate)
	if _, err := gate.begin(context.Background()); !errors.Is(err, errGatewayOperationsClosing) {
		t.Fatalf("begin while closing error = %v", err)
	}
	internalRelease, err := gate.begin(internalGatewayContext(context.Background()))
	if err != nil {
		t.Fatalf("internal begin returned error: %v", err)
	}
	internalRelease()
	release()
	release()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("closeAndWait returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("closeAndWait did not observe the released operation")
	}
	gate.reopen()
	reopenedRelease, err := gate.begin(context.Background())
	if err != nil {
		t.Fatalf("begin after reopen returned error: %v", err)
	}
	reopenedRelease()

	if err := (*operationGate)(nil).closeAndWait(context.Background()); err != nil {
		t.Fatalf("nil gate closeAndWait returned error: %v", err)
	}
	if _, err := (&gatewayBackend{}).beginGatewayOperation(context.Background()); err == nil {
		t.Fatal("gatewayBackend without a gate should fail")
	}
	if _, err := gate.begin(canceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled begin error = %v", err)
	}
}

func TestGatewayRoundTripQuiescesWorkspaceLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		run         func(context.Context, *Workspace) error
		gateReopens bool
	}{
		{
			name: "close",
			run: func(ctx context.Context, w *Workspace) error {
				return w.Close(ctx)
			},
		},
		{
			name: "reset",
			run: func(ctx context.Context, w *Workspace) error {
				return w.Reset(ctx)
			},
			gateReopens: true,
		},
		{
			name: "remove",
			run: func(ctx context.Context, w *Workspace) error {
				return w.RemoveMCP(ctx, "one")
			},
			gateReopens: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w, backend, _, gatewayClient, _ := readyWorkspace(t)
			config := httpMCPConfig("one")
			proxy := &fakeMCP{config: config, connected: true}
			w.mcps = []workspace.MCPClient{proxy}
			gatewayClient.configs = []workspace.MCPClientConfig{config}
			gate := w.gatewayGate
			transport, releaseOperation, operationDone := startBlockingGatewayRoundTrip(t, backend, gate)

			lifecycleDone := make(chan error, 1)
			go func() { lifecycleDone <- test.run(context.Background(), w) }()
			waitForGateClosing(t, gate)
			_, err := transport.RoundTrip(context.Background(), &gateway.Request{
				Method: http.MethodGet, Path: "/health", MaxResponseBytes: 1024,
			})
			if !errors.Is(err, errGatewayOperationsClosing) {
				t.Fatalf("new RoundTrip while %s is quiescing error = %v", test.name, err)
			}
			select {
			case err := <-lifecycleDone:
				releaseOperation()
				t.Fatalf("%s returned before the MCP request completed: %v", test.name, err)
			default:
			}

			releaseOperation()
			select {
			case err := <-operationDone:
				if err != nil {
					t.Fatalf("blocking RoundTrip returned error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("blocking RoundTrip did not finish")
			}
			select {
			case err := <-lifecycleDone:
				if err != nil {
					t.Fatalf("%s returned error: %v", test.name, err)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s remained blocked after the MCP request completed", test.name)
			}

			if proxy.IsConnected() {
				t.Fatalf("%s did not mark the removed proxy disconnected", test.name)
			}
			if _, err := proxy.ListTools(context.Background()); err == nil {
				t.Fatalf("old proxy remained usable after %s", test.name)
			}
			if test.gateReopens {
				release, err := gate.begin(context.Background())
				if err != nil {
					t.Fatalf("gateway gate did not reopen after %s: %v", test.name, err)
				}
				release()
			} else {
				_, err := transport.RoundTrip(context.Background(), &gateway.Request{
					Method: http.MethodGet, Path: "/health", MaxResponseBytes: 1024,
				})
				if !errors.Is(err, errGatewayOperationsClosing) {
					t.Fatalf("old gateway proxy after Close error = %v", err)
				}
			}
		})
	}
}

func TestCloseTimeoutReopensGatewayOperations(t *testing.T) {
	t.Parallel()

	w, _, provider, gatewayClient, _ := readyWorkspace(t)
	gate := w.gatewayGate
	release, err := gate.begin(context.Background())
	if err != nil {
		t.Fatalf("begin returned error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = w.Close(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close timeout error = %v", err)
	}
	_, providerCloses := provider.counts()
	if providerCloses != 0 || gatewayClient.closeCalls != 0 || !w.IsAlive() {
		t.Fatalf("timed-out Close mutated lifecycle: provider=%d gateway=%d alive=%t", providerCloses, gatewayClient.closeCalls, w.IsAlive())
	}
	reopenedRelease, err := gate.begin(context.Background())
	if err != nil {
		t.Fatalf("gateway gate remained closed after timeout: %v", err)
	}
	reopenedRelease()
	release()
	if err := w.Close(context.Background()); err != nil {
		t.Fatalf("Close after releasing operation returned error: %v", err)
	}
}

func startBlockingGatewayRoundTrip(
	t *testing.T,
	backend *fakeBackend,
	gate *operationGate,
) (*pythonLoopbackTransport, func(), <-chan error) {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	backend.execHook = func(ctx context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
		if len(argv) == 0 || argv[0] != "python3" {
			return ExecResult{}, nil, false
		}
		close(started)
		select {
		case <-release:
			return ExecResult{ExitCode: 0, Stdout: []byte(`{"status":200,"body":""}`)}, nil, true
		case <-ctx.Done():
			return ExecResult{}, ctx.Err(), true
		}
	}
	wrapped := &gatewayBackend{Backend: backend, gate: gate}
	transport := &pythonLoopbackTransport{
		backend: wrapped,
		port:    5600,
		timeout: time.Second,
		leaser:  wrapped,
	}
	done := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(context.Background(), &gateway.Request{
			Method: http.MethodGet, Path: "/health", MaxResponseBytes: 1024,
		})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("RoundTrip did not reach the backend")
	}
	return transport, func() { releaseOnce.Do(func() { close(release) }) }, done
}

func waitForGateClosing(t *testing.T, gate *operationGate) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		gate.mu.Lock()
		closing := gate.closing
		gate.mu.Unlock()
		if closing {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("gateway gate did not enter closing state")
		}
	}
}
