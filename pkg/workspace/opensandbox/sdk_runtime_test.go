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
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/alibaba/OpenSandbox/sdks/sandbox/go"
)

type sdkRuntimeServer struct {
	mu sync.Mutex

	lifecycle *httptest.Server
	execd     *httptest.Server

	createRequest   sdk.CreateSandboxRequest
	commandRequest  sdk.RunCommandRequest
	commandMode     string
	listPages       int
	resumeCalls     int
	pauseCalls      int
	directoryCalls  int
	uploadCalls     int
	downloadPayload []byte
}

func newSDKRuntimeServer(t *testing.T) *sdkRuntimeServer {
	t.Helper()
	server := &sdkRuntimeServer{downloadPayload: []byte("downloaded")}
	server.execd = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.handleExecd(t, writer, request)
	}))
	server.lifecycle = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.handleLifecycle(t, writer, request)
	}))
	t.Cleanup(server.lifecycle.Close)
	t.Cleanup(server.execd.Close)
	return server
}

func (s *sdkRuntimeServer) connection() sdk.ConnectionConfig {
	return sdk.ConnectionConfig{
		Domain:         s.lifecycle.URL,
		Protocol:       "http",
		APIKey:         "test-key",
		RequestTimeout: time.Second,
	}
}

func (s *sdkRuntimeServer) handleLifecycle(
	t *testing.T,
	writer http.ResponseWriter,
	request *http.Request,
) {
	t.Helper()
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/v1/sandboxes":
		page := request.URL.Query().Get("page")
		s.mu.Lock()
		s.listPages++
		s.mu.Unlock()
		if page == "2" {
			writeSDKJSON(t, writer, http.StatusOK, sdk.ListSandboxesResponse{
				Items: []sdk.SandboxInfo{{
					ID:        "paused",
					Status:    sdk.SandboxStatus{State: sdk.StatePaused},
					CreatedAt: time.Unix(2, 0).UTC(),
				}},
				Pagination: sdk.PaginationInfo{Page: 2},
			})
			return
		}
		writeSDKJSON(t, writer, http.StatusOK, sdk.ListSandboxesResponse{
			Items: []sdk.SandboxInfo{{
				ID:        "running",
				Status:    sdk.SandboxStatus{State: sdk.StateRunning},
				CreatedAt: time.Unix(1, 0).UTC(),
			}},
			Pagination: sdk.PaginationInfo{Page: 1, HasNextPage: true},
		})
	case request.Method == http.MethodPost && request.URL.Path == "/v1/sandboxes":
		var createRequest sdk.CreateSandboxRequest
		if err := json.NewDecoder(request.Body).Decode(&createRequest); err != nil {
			t.Errorf("decode create request: %v", err)
		}
		s.mu.Lock()
		s.createRequest = createRequest
		s.mu.Unlock()
		writeSDKJSON(t, writer, http.StatusCreated, sdk.SandboxInfo{
			ID:        "created",
			Status:    sdk.SandboxStatus{State: sdk.StateRunning},
			CreatedAt: time.Now().UTC(),
		})
	case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/endpoints/44772"):
		writeSDKJSON(t, writer, http.StatusOK, sdk.Endpoint{
			Endpoint: s.execd.URL,
			Headers:  map[string]string{"X-EXECD-ACCESS-TOKEN": "token"},
		})
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/sandboxes/"):
		parts := strings.Split(request.URL.Path, "/")
		writeSDKJSON(t, writer, http.StatusOK, sdk.SandboxInfo{
			ID:        parts[len(parts)-1],
			Status:    sdk.SandboxStatus{State: sdk.StateRunning},
			CreatedAt: time.Now().UTC(),
		})
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/resume"):
		s.mu.Lock()
		s.resumeCalls++
		s.mu.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/pause"):
		s.mu.Lock()
		s.pauseCalls++
		s.mu.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(writer, request)
	}
}

func (s *sdkRuntimeServer) handleExecd(
	t *testing.T,
	writer http.ResponseWriter,
	request *http.Request,
) {
	t.Helper()
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/ping":
		writer.WriteHeader(http.StatusOK)
	case request.Method == http.MethodPost && request.URL.Path == "/command":
		var command sdk.RunCommandRequest
		if err := json.NewDecoder(request.Body).Decode(&command); err != nil {
			t.Errorf("decode command request: %v", err)
		}
		s.mu.Lock()
		s.commandRequest = command
		mode := s.commandMode
		s.mu.Unlock()
		if mode == "http-error" {
			http.Error(writer, "command failed", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		if mode == "execution-error" {
			_, _ = io.WriteString(writer,
				`{"type":"stderr","text":"warning","timestamp":1}`+"\n\n"+
					`{"type":"error","error":{"ename":"ExitError","evalue":"2"},"timestamp":2}`+"\n\n"+
					`{"type":"execution_complete","timestamp":3}`+"\n\n")
			return
		}
		_, _ = io.WriteString(writer,
			`{"type":"stdout","text":"one","timestamp":1}`+"\n\n"+
				`{"type":"stdout","text":"two","timestamp":2}`+"\n\n"+
				`{"type":"stderr","text":"warning","timestamp":3}`+"\n\n"+
				`{"type":"execution_complete","timestamp":4}`+"\n\n")
	case request.Method == http.MethodGet && request.URL.Path == "/files/download":
		s.mu.Lock()
		payload := append([]byte(nil), s.downloadPayload...)
		s.mu.Unlock()
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(payload)
	case request.Method == http.MethodPost && request.URL.Path == "/directories":
		s.mu.Lock()
		s.directoryCalls++
		s.mu.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	case request.Method == http.MethodPost && request.URL.Path == "/files/upload":
		_, _ = io.Copy(io.Discard, request.Body)
		s.mu.Lock()
		s.uploadCalls++
		s.mu.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(writer, request)
	}
}

func writeSDKJSON(t *testing.T, writer http.ResponseWriter, status int, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode SDK response: %v", err)
	}
}

func TestSDKRuntimeLifecycleAndHandleOperations(t *testing.T) {
	server := newSDKRuntimeServer(t)
	runtime := &sdkRuntime{}
	connection := server.connection()

	infos, err := runtime.List(context.Background(), connection, "workspace-id")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	server.mu.Lock()
	listPages := server.listPages
	server.mu.Unlock()
	if len(infos) != 2 || infos[0].ID != "running" || infos[1].ID != "paused" || listPages != 2 {
		t.Fatalf("List result mismatch: infos=%#v pages=%d", infos, listPages)
	}

	spec := sandboxSpec{
		ID:         "workspace-id",
		Image:      "python:3.11-slim",
		Connection: connection,
		Timeout:    1500 * time.Millisecond,
		Env:        map[string]string{"MODE": "test"},
		Metadata:   map[string]string{metadataWorkspaceID: "workspace-id"},
		ResourceLimits: sdk.ResourceLimits{
			"cpu": "1",
		},
		Entrypoint: []string{"sleep", "infinity"},
		NetworkPolicy: &sdk.NetworkPolicy{
			DefaultAction: "deny",
		},
	}
	created, err := runtime.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	connected, err := runtime.Connect(context.Background(), connection, "running", 2*time.Second)
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	resumed, err := runtime.Resume(context.Background(), connection, "paused", 2*time.Second)
	if err != nil {
		t.Fatalf("Resume returned error: %v", err)
	}
	if created.ID() != "created" || connected.ID() != "running" || resumed.ID() != "paused" {
		t.Fatalf("handle IDs = %q, %q, %q", created.ID(), connected.ID(), resumed.ID())
	}
	server.mu.Lock()
	createRequest := server.createRequest
	resumeCalls := server.resumeCalls
	server.mu.Unlock()
	if createRequest.Image == nil || createRequest.Image.URI != spec.Image ||
		createRequest.Timeout == nil || *createRequest.Timeout != 2 ||
		!reflect.DeepEqual(createRequest.Env, spec.Env) ||
		createRequest.Metadata[metadataWorkspaceID] != "workspace-id" || resumeCalls != 1 {
		t.Fatalf("create/resume requests mismatch: request=%#v resumes=%d", createRequest, resumeCalls)
	}

	handle := created.(*sdkHandle)
	healthy, err := handle.Healthy(context.Background())
	if err != nil || !healthy {
		t.Fatalf("Healthy = %v, %v", healthy, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handle.Healthy(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Healthy error = %v", err)
	}

	result, err := handle.Run(
		context.Background(),
		[]string{"printf", "a b", "it's"},
		"/workspace",
		map[string]string{"TOKEN": "value"},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ExitCode != 0 || string(result.Stdout) != "one\ntwo" || string(result.Stderr) != "warning" {
		t.Fatalf("Run result mismatch: %#v", result)
	}
	server.mu.Lock()
	commandRequest := server.commandRequest
	server.mu.Unlock()
	if commandRequest.Command != `'printf' 'a b' 'it'"'"'s'` || commandRequest.Cwd != "/workspace" ||
		commandRequest.Envs["TOKEN"] != "value" || commandRequest.Timeout != 5000 {
		t.Fatalf("command request mismatch: %#v", commandRequest)
	}

	server.mu.Lock()
	server.commandMode = "execution-error"
	server.mu.Unlock()
	result, err = handle.Run(context.Background(), []string{"false"}, "/", nil, 0)
	if err != nil || result.ExitCode != 2 || !strings.Contains(string(result.Stderr), "ExitError: 2") {
		t.Fatalf("execution-error result = %#v, %v", result, err)
	}
	server.mu.Lock()
	server.commandMode = "http-error"
	server.mu.Unlock()
	if _, err := handle.Run(context.Background(), []string{"false"}, "/", nil, 0); err == nil {
		t.Fatal("HTTP command failure should return an error")
	}

	data, err := handle.ReadFile(context.Background(), "/workspace/input.txt")
	if err != nil || string(data) != "downloaded" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	server.mu.Lock()
	server.downloadPayload = []byte("12345")
	server.mu.Unlock()
	data, err = handle.ReadFileLimit(context.Background(), "/workspace/input.txt", 5)
	if err != nil || string(data) != "12345" {
		t.Fatalf("ReadFileLimit exact boundary = %q, %v", data, err)
	}
	server.mu.Lock()
	server.downloadPayload = []byte("123456")
	server.mu.Unlock()
	if _, err := handle.ReadFileLimit(context.Background(), "/workspace/input.txt", 5); err == nil ||
		!strings.Contains(err.Error(), "exceeds 5 bytes") {
		t.Fatalf("ReadFileLimit over-limit error = %v", err)
	}
	for _, limit := range []int64{-1, 0, math.MaxInt64} {
		if _, err := handle.ReadFileLimit(context.Background(), "/workspace/input.txt", limit); err == nil {
			t.Fatalf("ReadFileLimit(%d) should return an error", limit)
		}
	}
	if err := handle.WriteFile(context.Background(), "/workspace/dir/output.txt", []byte("payload")); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := handle.WriteFile(context.Background(), "/output.txt", []byte("payload")); err != nil {
		t.Fatalf("root WriteFile returned error: %v", err)
	}
	server.mu.Lock()
	directoryCalls := server.directoryCalls
	uploadCalls := server.uploadCalls
	server.mu.Unlock()
	if directoryCalls != 1 || uploadCalls != 2 {
		t.Fatalf("file operation calls: directories=%d uploads=%d", directoryCalls, uploadCalls)
	}

	if err := handle.Pause(context.Background()); err != nil {
		t.Fatalf("Pause returned error: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	server.mu.Lock()
	pauseCalls := server.pauseCalls
	server.mu.Unlock()
	if pauseCalls != 1 {
		t.Fatalf("pause calls = %d, want 1", pauseCalls)
	}
}

func TestSDKRuntimeFailurePaths(t *testing.T) {
	failureServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "failed", http.StatusInternalServerError)
	}))
	defer failureServer.Close()
	connection := sdk.ConnectionConfig{Domain: failureServer.URL, Protocol: "http", RequestTimeout: time.Second}
	runtime := &sdkRuntime{}

	if _, err := runtime.List(context.Background(), connection, "workspace-id"); err == nil {
		t.Fatal("List should propagate an HTTP error")
	}
	if _, err := runtime.Create(context.Background(), sandboxSpec{
		Image:      "python:3.11-slim",
		Connection: connection,
		Timeout:    time.Second,
	}); err == nil {
		t.Fatal("Create should propagate an HTTP error")
	}
	if _, err := runtime.Connect(context.Background(), connection, "sandbox", time.Second); err == nil {
		t.Fatal("Connect should propagate an HTTP error")
	}
	if _, err := runtime.Resume(context.Background(), connection, "sandbox", time.Second); err == nil {
		t.Fatal("Resume should propagate an HTTP error")
	}
	if _, err := runtime.Create(context.Background(), sandboxSpec{Timeout: 0}); err == nil {
		t.Fatal("Create should reject a non-positive timeout")
	}
}

func TestSDKHandleNilSafetyAndHelpers(t *testing.T) {
	for _, handle := range []*sdkHandle{nil, {}} {
		if handle.ID() != "" {
			t.Fatalf("nil SDK handle ID = %q", handle.ID())
		}
		if _, err := handle.Healthy(context.Background()); err == nil {
			t.Fatal("nil Healthy should return an error")
		}
		if _, err := handle.Run(context.Background(), []string{"true"}, "/", nil, 0); err == nil {
			t.Fatal("nil Run should return an error")
		}
		if _, err := handle.ReadFile(context.Background(), "/file"); err == nil {
			t.Fatal("nil ReadFile should return an error")
		}
		if _, err := handle.ReadFileLimit(context.Background(), "/file", 1); err == nil {
			t.Fatal("nil ReadFileLimit should return an error")
		}
		if err := handle.WriteFile(context.Background(), "/file", nil); err == nil {
			t.Fatal("nil WriteFile should return an error")
		}
		if err := handle.Pause(context.Background()); err != nil {
			t.Fatalf("nil Pause returned error: %v", err)
		}
		if err := handle.Close(); err != nil {
			t.Fatalf("nil Close returned error: %v", err)
		}
	}

	if got := shellQuote(""); got != "''" {
		t.Fatalf("shellQuote empty = %q", got)
	}
	if got := shellJoin([]string{"printf", "a b", "it's"}); got != `'printf' 'a b' 'it'"'"'s'` {
		t.Fatalf("shellJoin = %q", got)
	}
	messages := []sdk.OutputMessage{{Text: "one"}, {}, {Text: "\ntwo"}, {Text: "three"}}
	if got := joinOutputMessages(messages); got != "one\ntwo\nthree" {
		t.Fatalf("joinOutputMessages = %q", got)
	}
	if _, err := durationSeconds(0); err == nil {
		t.Fatal("durationSeconds should reject zero")
	}
	if seconds, err := durationSeconds(time.Nanosecond); err != nil || seconds != 1 {
		t.Fatalf("durationSeconds(1ns) = %d, %v", seconds, err)
	}
	if seconds, err := durationSeconds(1500 * time.Millisecond); err != nil || seconds != 2 {
		t.Fatalf("durationSeconds(1.5s) = %d, %v", seconds, err)
	}
}
