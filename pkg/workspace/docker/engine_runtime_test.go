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

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
)

func TestEngineRuntimeUsesDockerAPIForContainerFileAndExecOperations(t *testing.T) {
	t.Parallel()

	api := newFakeDockerAPI(t)
	defer api.server.Close()

	client, err := mobyclient.NewClientWithOpts(
		mobyclient.WithHost("tcp://"+api.server.Listener.Addr().String()),
		mobyclient.WithVersion("1.45"),
	)
	if err != nil {
		t.Fatalf("NewClientWithOpts returned error: %v", err)
	}
	runtime := &engineRuntime{client: client}
	ctx := context.Background()

	containerID, err := runtime.Create(ctx, containerSpec{
		ID:              "workspace-1",
		Name:            "agentscope-workspace",
		Image:           "ubuntu:test",
		User:            "1000:1000",
		Workdir:         "/workspace",
		HostWorkdir:     "/tmp/agentscope-host",
		NetworkDisabled: true,
		MemoryBytes:     1024,
		NanoCPUs:        2_000_000_000,
		StopTimeout:     1500 * time.Millisecond,
		Env:             map[string]string{"B": "2", "A": "1"},
		ExtraLabels:     map[string]string{"custom": "label"},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if containerID != "container-123" {
		t.Fatalf("container ID mismatch: %q", containerID)
	}
	if api.createRequest["Name"] != "agentscope-workspace" {
		t.Fatalf("container name should be passed as query: %#v", api.createRequest)
	}
	config := api.createRequest
	hostConfig := api.createRequest["HostConfig"].(map[string]any)
	if config["Image"] != "ubuntu:test" || config["User"] != "1000:1000" || config["WorkingDir"] != "/workspace" {
		t.Fatalf("container config mismatch: %#v", config)
	}
	if config["NetworkDisabled"] != true || hostConfig["NetworkMode"] != "none" {
		t.Fatalf("network disabled settings mismatch: config=%#v host=%#v", config, hostConfig)
	}
	if labels := config["Labels"].(map[string]any); labels["custom"] != "label" || labels["agentscope-go.workspace.id"] != "workspace-1" {
		t.Fatalf("labels mismatch: %#v", labels)
	}

	if err := runtime.Start(ctx, containerID); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	pulledID, err := runtime.Create(ctx, containerSpec{
		ID:        "workspace-2",
		Name:      "agentscope-pulled",
		Image:     "ubuntu:test",
		PullImage: true,
	})
	if err != nil {
		t.Fatalf("Create with PullImage returned error: %v", err)
	}
	if pulledID != "container-123" || !api.imagePulled {
		t.Fatalf("Create with PullImage mismatch: id=%q imagePulled=%v", pulledID, api.imagePulled)
	}
	if err := runtime.Stop(ctx, containerID); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if err := runtime.Remove(ctx, containerID); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if !api.started || !api.stopped || !api.removed {
		t.Fatalf("lifecycle calls not observed: start=%v stop=%v remove=%v", api.started, api.stopped, api.removed)
	}

	read, err := runtime.ReadFile(ctx, containerID, "/workspace/out.txt")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(read) != "from-container" {
		t.Fatalf("ReadFile data mismatch: %q", string(read))
	}
	if err := runtime.WriteFile(ctx, containerID, "/workspace/in.txt", []byte("to-container"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if string(api.writtenFile) != "to-container" {
		t.Fatalf("WriteFile archive data mismatch: %q", string(api.writtenFile))
	}

	result, err := runtime.Run(ctx, containerID, runRequest{
		Command: "echo ok",
		User:    "1000:1000",
		Workdir: "/workspace",
		Env:     map[string]string{"FOO": "bar"},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Stdout != "hello\n" || result.Stderr != "warn\n" || result.ExitCode != 7 {
		t.Fatalf("Run result mismatch: %#v", result)
	}
	if api.execCreate["WorkingDir"] != "/workspace" || api.execCreate["User"] != "1000:1000" {
		t.Fatalf("exec create body mismatch: %#v", api.execCreate)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := (*engineRuntime)(nil).Close(); err != nil {
		t.Fatalf("nil Close should be nil, got %v", err)
	}
}

func TestEngineRuntimeTarFileAndHelperBranches(t *testing.T) {
	t.Parallel()

	for _, filePath := range []string{"", "/", "."} {
		if _, err := tarFile(filePath, nil, 0o644); err == nil {
			t.Fatalf("tarFile(%q) should reject invalid target", filePath)
		}
	}
	if got := envList(nil); got != nil {
		t.Fatalf("nil env should produce nil list, got %#v", got)
	}
	if got := durationSecondsPtr(0); got != nil {
		t.Fatalf("zero duration should produce nil pointer, got %#v", got)
	}
	if got := durationSecondsPtr(time.Millisecond); got == nil || *got != 1 {
		t.Fatalf("subsecond duration should round up to one second, got %#v", got)
	}
	merged := labels(containerSpec{
		ID:          "workspace-1",
		ExtraLabels: map[string]string{"agentscope-go.workspace": "override", "x": "y"},
	})
	if merged["agentscope-go.workspace"] != "override" || merged["agentscope-go.workspace.id"] != "workspace-1" || merged["x"] != "y" {
		t.Fatalf("labels should merge extra labels last: %#v", merged)
	}
}

type fakeDockerAPI struct {
	t             *testing.T
	server        *httptest.Server
	createRequest map[string]any
	execCreate    map[string]any
	started       bool
	stopped       bool
	removed       bool
	imagePulled   bool
	writtenFile   []byte
}

func newFakeDockerAPI(t *testing.T) *fakeDockerAPI {
	t.Helper()
	api := &fakeDockerAPI{t: t}
	api.server = httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	return api
}

func (api *fakeDockerAPI) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch dockerAPIPath(r.URL.Path) {
	case "/images/create":
		api.handleImagePull(w, r)
	case "/containers/create":
		api.handleCreate(w, r)
	case "/containers/container-123/start":
		api.started = true
		w.WriteHeader(http.StatusNoContent)
	case "/containers/container-123/stop":
		api.stopped = true
		w.WriteHeader(http.StatusNoContent)
	case "/containers/container-123":
		if r.Method != http.MethodDelete {
			api.t.Fatalf("unexpected container method: %s", r.Method)
		}
		if r.URL.Query().Get("force") != "1" || r.URL.Query().Get("v") != "1" {
			api.t.Fatalf("remove query mismatch: %s", r.URL.RawQuery)
		}
		api.removed = true
		w.WriteHeader(http.StatusNoContent)
	case "/containers/container-123/archive":
		api.handleArchive(w, r)
	case "/containers/container-123/exec":
		api.handleExecCreate(w, r)
	case "/exec/exec-123/start":
		api.handleExecAttach(w, r)
	case "/exec/exec-123/json":
		_ = json.NewEncoder(w).Encode(map[string]any{"ID": "exec-123", "ContainerID": "container-123", "ExitCode": 7})
	default:
		api.t.Fatalf("unexpected Docker API request: %s %s", r.Method, r.URL.String())
	}
}

func (api *fakeDockerAPI) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		api.t.Fatalf("unexpected create method: %s", r.Method)
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.t.Fatalf("decode create body: %v", err)
	}
	body["Name"] = r.URL.Query().Get("name")
	api.createRequest = body
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"Id": "container-123", "Warnings": []string{}})
}

func (api *fakeDockerAPI) handleImagePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		api.t.Fatalf("unexpected image pull method: %s", r.Method)
	}
	fromImage := r.URL.Query().Get("fromImage")
	if fromImage != "ubuntu" && fromImage != "docker.io/library/ubuntu" || r.URL.Query().Get("tag") != "test" {
		api.t.Fatalf("image pull query mismatch: %s", r.URL.RawQuery)
	}
	api.imagePulled = true
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{\"status\":\"done\"}\n"))
}

func (api *fakeDockerAPI) handleArchive(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.writeContainerArchive(w)
	case http.MethodPut:
		api.readContainerArchive(r)
		w.WriteHeader(http.StatusOK)
	default:
		api.t.Fatalf("unexpected archive method: %s", r.Method)
	}
}

func (api *fakeDockerAPI) writeContainerArchive(w http.ResponseWriter) {
	stat, err := json.Marshal(container.PathStat{Name: "out.txt", Size: int64(len("from-container")), Mode: 0o644})
	if err != nil {
		api.t.Fatalf("marshal path stat: %v", err)
	}
	w.Header().Set("X-Docker-Container-Path-Stat", base64.StdEncoding.EncodeToString(stat))
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "out.txt", Typeflag: tar.TypeReg, Size: int64(len("from-container")), Mode: 0o644}); err != nil {
		api.t.Fatalf("write archive header: %v", err)
	}
	if _, err := writer.Write([]byte("from-container")); err != nil {
		api.t.Fatalf("write archive body: %v", err)
	}
	if err := writer.Close(); err != nil {
		api.t.Fatalf("close archive writer: %v", err)
	}
	_, _ = w.Write(buffer.Bytes())
}

func (api *fakeDockerAPI) readContainerArchive(r *http.Request) {
	reader := tar.NewReader(r.Body)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			api.t.Fatalf("read uploaded archive: %v", err)
		}
		if header.FileInfo().IsDir() {
			continue
		}
		api.writtenFile, err = io.ReadAll(reader)
		if err != nil {
			api.t.Fatalf("read uploaded file: %v", err)
		}
		return
	}
	api.t.Fatalf("uploaded archive did not contain a file")
}

func (api *fakeDockerAPI) handleExecCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		api.t.Fatalf("unexpected exec create method: %s", r.Method)
	}
	if err := json.NewDecoder(r.Body).Decode(&api.execCreate); err != nil {
		api.t.Fatalf("decode exec create body: %v", err)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"Id": "exec-123"})
}

func (api *fakeDockerAPI) handleExecAttach(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Upgrade") == "" {
		api.t.Fatalf("exec attach should upgrade connection")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		api.t.Fatalf("response writer does not support hijacking")
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		api.t.Fatalf("hijack exec attach: %v", err)
	}
	tcpConn, _ := conn.(*net.TCPConn)
	_, _ = conn.Write([]byte("HTTP/1.1 101 UPGRADED\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Type: application/vnd.docker.raw-stream\r\n\r\n"))
	_, _ = conn.Write(dockerStdCopyFrame(1, []byte("hello\n")))
	_, _ = conn.Write(dockerStdCopyFrame(2, []byte("warn\n")))
	if tcpConn != nil {
		_ = tcpConn.CloseWrite()
		_ = tcpConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, _ = io.Copy(io.Discard, tcpConn)
	}
	_ = conn.Close()
}

func dockerAPIPath(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 2 && strings.HasPrefix(parts[0], "v") && strings.Contains(parts[0], ".") {
		return "/" + parts[1]
	}
	return path
}

func dockerStdCopyFrame(stream byte, data []byte) []byte {
	frame := make([]byte, 8+len(data))
	frame[0] = stream
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(data)))
	copy(frame[8:], data)
	return frame
}
