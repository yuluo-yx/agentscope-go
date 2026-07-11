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
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/workspace/gateway"
)

func TestNewPythonGatewayValidation(t *testing.T) {
	t.Parallel()

	if _, err := NewPythonGateway(nil, 5600, time.Second); err == nil {
		t.Fatal("nil backend should fail")
	}
	backend := newFakeBackend()
	for _, port := range []int{0, -1, 65536} {
		if _, err := NewPythonGateway(backend, port, time.Second); err == nil {
			t.Fatalf("port %d should fail", port)
		}
	}
	client, err := NewPythonGateway(backend, 5600, 0)
	if err != nil {
		t.Fatalf("NewPythonGateway returned error: %v", err)
	}
	if client == nil {
		t.Fatal("NewPythonGateway returned nil client")
	}
}

func TestPythonLoopbackTransportValidation(t *testing.T) {
	t.Parallel()

	valid := &gateway.Request{
		Method:           http.MethodGet,
		Path:             "/health",
		MaxResponseBytes: 1024,
	}
	tests := []struct {
		name      string
		transport *pythonLoopbackTransport
		ctx       context.Context
		request   *gateway.Request
		want      string
	}{
		{name: "nil receiver", ctx: context.Background(), request: valid, want: "nil loopback transport"},
		{name: "nil backend", transport: &pythonLoopbackTransport{}, ctx: context.Background(), request: valid, want: "nil loopback transport"},
		{name: "nil context", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, request: valid, want: "nil loopback context"},
		{name: "canceled context", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, ctx: canceledContext(), request: valid, want: "context canceled"},
		{name: "nil request", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, ctx: context.Background(), want: "nil loopback request"},
		{name: "relative path", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, ctx: context.Background(), request: &gateway.Request{Method: http.MethodGet, Path: "health"}, want: "invalid loopback path"},
		{name: "network path", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, ctx: context.Background(), request: &gateway.Request{Method: http.MethodGet, Path: "//evil"}, want: "invalid loopback path"},
		{name: "method", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, ctx: context.Background(), request: &gateway.Request{Method: http.MethodPut, Path: "/health"}, want: "unsupported loopback method"},
		{name: "header name", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, ctx: context.Background(), request: &gateway.Request{Method: http.MethodPost, Path: "/mcps", Header: http.Header{"Authorization": {"secret"}}}, want: "unsupported headers"},
		{name: "header count", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, ctx: context.Background(), request: &gateway.Request{Method: http.MethodPost, Path: "/mcps", Header: http.Header{"Content-Type": {"application/json", "text/plain"}}}, want: "unsupported headers"},
		{name: "header value", transport: &pythonLoopbackTransport{backend: newFakeBackend()}, ctx: context.Background(), request: &gateway.Request{Method: http.MethodPost, Path: "/mcps", Header: http.Header{"content-type": {"text/plain"}}}, want: "unsupported headers"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.transport.RoundTrip(test.ctx, test.request)
			requireErrorContains(t, err, test.want)
		})
	}
}

func TestPythonLoopbackTransportInlineRequestAndCleanup(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	var requestFile string
	backend.execHook = func(_ context.Context, argv []string, options ExecOptions) (ExecResult, error, bool) {
		if len(argv) == 0 || argv[0] != "python3" {
			return ExecResult{}, nil, false
		}
		if len(argv) != 9 ||
			argv[3] != http.MethodPost ||
			argv[4] != "http://127.0.0.1:5600/mcps" ||
			argv[6] != strconv.FormatInt(loopbackInlineLimit, 10) ||
			argv[7] != loopbackTempDir ||
			argv[8] != "1024" {
			t.Fatalf("unexpected shim argv: %#v", argv)
		}
		requestFile = argv[5]
		if got := string(backend.file(requestFile)); got != `{"name":"weather"}` {
			t.Fatalf("request temp file = %q", got)
		}
		if options.CWD != "/" || options.Timeout != 2*time.Second {
			t.Fatalf("unexpected exec options: %#v", options)
		}
		body := base64.StdEncoding.EncodeToString([]byte(`{"ok":true}`))
		return ExecResult{ExitCode: 0, Stdout: []byte(`{"status":201,"body":"` + body + `"}`)}, nil, true
	}
	transport := &pythonLoopbackTransport{backend: backend, port: 5600, timeout: 2 * time.Second}
	response, err := transport.RoundTrip(context.Background(), &gateway.Request{
		Method:           http.MethodPost,
		Path:             "/mcps",
		Header:           http.Header{"Content-Type": {"application/json"}},
		Body:             []byte(`{"name":"weather"}`),
		MaxResponseBytes: 1024,
	})
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	if response.StatusCode != http.StatusCreated || string(response.Body) != `{"ok":true}` {
		t.Fatalf("unexpected response: %#v", response)
	}
	if requestFile == "" || backend.file(requestFile) != nil {
		t.Fatalf("request temp file was not cleaned: %q", requestFile)
	}
	unlinks := backend.callsFor("unlink")
	if len(unlinks) != 1 || unlinks[0].argv[2] != requestFile {
		t.Fatalf("unexpected cleanup calls: %#v", unlinks)
	}
}

func TestPythonLoopbackTransportBodyFileCleanupAndLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    []byte
		limit   int64
		readErr error
		wantErr string
	}{
		{name: "success", body: []byte("large-response"), limit: 100},
		{name: "too large", body: bytes.Repeat([]byte("x"), 64*1024), limit: 1024, wantErr: "exceeds"},
		{name: "read error", body: []byte("large-response"), limit: 100, readErr: errors.New("read failed"), wantErr: "read failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := newFakeBackend()
			const filename = "/tmp/response.bin"
			backend.files[filename] = append([]byte(nil), test.body...)
			backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
				if len(argv) > 0 && argv[0] == "python3" {
					return ExecResult{ExitCode: 0, Stdout: []byte(`{"status":200,"body_file":"` + filename + `"}`)}, nil, true
				}
				return ExecResult{}, nil, false
			}
			if test.readErr != nil {
				backend.readHook = func(context.Context, string) ([]byte, error, bool) {
					return nil, test.readErr, true
				}
			}
			transport := &pythonLoopbackTransport{backend: backend, port: 5600, timeout: time.Second}
			response, err := transport.RoundTrip(context.Background(), &gateway.Request{
				Method: http.MethodGet, Path: "/tools", MaxResponseBytes: test.limit,
			})
			if test.wantErr != "" {
				requireErrorContains(t, err, test.wantErr)
			} else if err != nil || string(response.Body) != string(test.body) {
				t.Fatalf("RoundTrip response = %#v, %v", response, err)
			}
			if backend.file(filename) != nil {
				t.Fatalf("body file was not cleaned after %s", test.name)
			}
		})
	}
}

func TestPythonLoopbackTransportUsesLimitedFileReader(t *testing.T) {
	t.Parallel()

	const filename = "/tmp/limited-response.bin"
	backend := newFakeBackend()
	backend.files[filename] = []byte("stored")
	backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
		if len(argv) > 0 && argv[0] == "python3" {
			return ExecResult{ExitCode: 0, Stdout: []byte(`{"status":200,"body_file":"` + filename + `"}`)}, nil, true
		}
		return ExecResult{}, nil, false
	}
	limited := &recordingLimitedBackend{fakeBackend: backend, body: []byte("limited body")}
	transport := &pythonLoopbackTransport{backend: limited, port: 5600, timeout: time.Second}
	response, err := transport.RoundTrip(context.Background(), &gateway.Request{
		Method: http.MethodGet, Path: "/tools", MaxResponseBytes: 321,
	})
	if err != nil || string(response.Body) != "limited body" {
		t.Fatalf("RoundTrip response = %#v, %v", response, err)
	}
	if limited.filename != filename || limited.maxBytes != 321 {
		t.Fatalf("ReadFileLimit arguments = %q/%d", limited.filename, limited.maxBytes)
	}
	if backend.file(filename) != nil {
		t.Fatal("limited response file was not cleaned")
	}
}

func TestPythonLoopbackTransportBackendFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*fakeBackend)
		body      []byte
		want      string
	}{
		{
			name: "write request",
			configure: func(backend *fakeBackend) {
				backend.writeHook = func(context.Context, string, []byte) (error, bool) {
					return errors.New("disk full"), true
				}
			},
			body: []byte("{}"),
			want: "write loopback request",
		},
		{
			name: "exec error",
			configure: func(backend *fakeBackend) {
				backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
					return ExecResult{}, errors.New("exec unavailable"), true
				}
			},
			want: "execute loopback shim",
		},
		{
			name: "nonzero exit",
			configure: func(backend *fakeBackend) {
				backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
					return ExecResult{ExitCode: 7, Stderr: []byte("python failed")}, nil, true
				}
			},
			want: "exit code 7",
		},
		{
			name: "invalid envelope",
			configure: func(backend *fakeBackend) {
				backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
					return ExecResult{ExitCode: 0, Stdout: []byte("not-json")}, nil, true
				}
			},
			want: "decode loopback envelope",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := newFakeBackend()
			test.configure(backend)
			transport := &pythonLoopbackTransport{backend: backend, port: 5600, timeout: time.Second}
			_, err := transport.RoundTrip(context.Background(), &gateway.Request{
				Method: http.MethodPost, Path: "/mcps", Body: test.body, MaxResponseBytes: 100,
			})
			requireErrorContains(t, err, test.want)
		})
	}
}

func TestPythonLoopbackDeleteTempFileScope(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	transport := &pythonLoopbackTransport{backend: backend}
	transport.deleteTempFile(context.Background(), "")
	transport.deleteTempFile(context.Background(), "/work/not-temp")
	if calls := backend.callsFor("unlink"); len(calls) != 0 {
		t.Fatalf("out-of-scope files must not be unlinked: %#v", calls)
	}
}

type recordingLimitedBackend struct {
	*fakeBackend
	body     []byte
	filename string
	maxBytes int64
}

func (b *recordingLimitedBackend) ReadFileLimit(
	ctx context.Context,
	filename string,
	maxBytes int64,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.filename = filename
	b.maxBytes = maxBytes
	return bytes.Clone(b.body), nil
}
