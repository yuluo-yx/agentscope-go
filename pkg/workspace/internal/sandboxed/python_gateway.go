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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yuluo-yx/agentscope-go/pkg/workspace/gateway"
)

const (
	loopbackInlineLimit int64 = 4 * 1024 * 1024
	loopbackTempDir           = "/tmp"
	loopbackShimScript        = `
import sys, json, base64, uuid, os
import urllib.request, urllib.error

method = sys.argv[1]
url = sys.argv[2]
body_file = sys.argv[3]
inline_limit = int(sys.argv[4])
tmp_dir = sys.argv[5]
max_body_size = int(sys.argv[6])

body = None
if body_file:
    with open(body_file, "rb") as f:
        body = f.read()

req = urllib.request.Request(url, data=body, method=method)
if body is not None:
    req.add_header("Content-Type", "application/json")

try:
    with urllib.request.urlopen(req) as resp:
        status = int(resp.status)
        resp_body = resp.read(max_body_size + 1)
except urllib.error.HTTPError as e:
    status = int(e.code)
    try:
        resp_body = e.read(max_body_size + 1)
    except Exception:
        resp_body = b""
except Exception as e:
    json.dump(
        {"status": -1, "error": type(e).__name__ + ": " + str(e)},
        sys.stdout,
    )
    sys.exit(0)

if len(resp_body) > max_body_size:
    json.dump(
        {"status": -1, "error": "response body exceeds configured limit"},
        sys.stdout,
    )
    sys.exit(0)

env = {"status": status}
if len(resp_body) > inline_limit:
    p = os.path.join(tmp_dir, uuid.uuid4().hex + ".bin")
    with open(p, "wb") as f:
        f.write(resp_body)
    env["body_file"] = p
else:
    env["body"] = base64.b64encode(resp_body).decode("ascii")
json.dump(env, sys.stdout)
`
)

// NewPythonGateway creates a client that reaches the loopback gateway through an in-sandbox Python shim.
func NewPythonGateway(
	backend Backend,
	port int,
	timeout time.Duration,
) (Gateway, error) {
	if backend == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil loopback backend")
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("workspace/sandboxed: invalid loopback port")
	}
	if timeout <= 0 {
		timeout = defaultGatewayTimeout
	}
	transport := &pythonLoopbackTransport{
		backend: backend,
		port:    port,
		timeout: timeout,
	}
	transport.leaser, _ = backend.(gatewayOperationLeaser)
	return gateway.NewClient(
		transport,
		gateway.WithPythonMCPConfigJSON(),
		gateway.WithMaxResponseBytes(gateway.DefaultMaxResponseBytes),
	)
}

type pythonLoopbackTransport struct {
	backend Backend
	port    int
	timeout time.Duration
	leaser  gatewayOperationLeaser
}

func (t *pythonLoopbackTransport) RoundTrip(
	ctx context.Context,
	request *gateway.Request,
) (*gateway.Response, error) {
	if err := t.validateTransport(ctx, request); err != nil {
		return nil, err
	}
	if t.leaser != nil {
		release, err := t.leaser.beginGatewayOperation(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	if err := validateLoopbackRequest(request); err != nil {
		return nil, err
	}

	bodyFile := ""
	if len(request.Body) > 0 {
		bodyFile = loopbackTempDir + "/agentscope-request-" + uuid.NewString() + ".json"
		if err := t.backend.WriteFile(ctx, bodyFile, request.Body); err != nil {
			return nil, fmt.Errorf("workspace/sandboxed: write loopback request: %w", err)
		}
		defer func() {
			cleanupCtx, cancel := t.detachedContext(ctx)
			defer cancel()
			t.deleteTempFile(cleanupCtx, bodyFile)
		}()
	}

	requestURL := "http://127.0.0.1:" + strconv.Itoa(t.port) + request.Path
	result, err := t.backend.Exec(ctx, []string{
		"python3",
		"-c",
		loopbackShimScript,
		request.Method,
		requestURL,
		bodyFile,
		strconv.FormatInt(loopbackInlineLimit, 10),
		loopbackTempDir,
		strconv.FormatInt(request.MaxResponseBytes, 10),
	}, ExecOptions{CWD: "/", Timeout: t.timeout})
	if err != nil {
		return nil, fmt.Errorf("workspace/sandboxed: execute loopback shim: %w", err)
	}
	if !result.OK() {
		return nil, commandError("execute loopback shim", result)
	}

	return t.decodeResponse(ctx, result.Stdout, request.MaxResponseBytes)
}

func (t *pythonLoopbackTransport) validateTransport(
	ctx context.Context,
	request *gateway.Request,
) error {
	if t == nil || t.backend == nil {
		return fmt.Errorf("workspace/sandboxed: nil loopback transport")
	}
	if ctx == nil {
		return fmt.Errorf("workspace/sandboxed: nil loopback context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if request == nil {
		return fmt.Errorf("workspace/sandboxed: nil loopback request")
	}
	return nil
}

func validateLoopbackRequest(request *gateway.Request) error {
	if !strings.HasPrefix(request.Path, "/") || strings.HasPrefix(request.Path, "//") {
		return fmt.Errorf("workspace/sandboxed: invalid loopback path")
	}
	if request.Method != http.MethodGet &&
		request.Method != http.MethodPost &&
		request.Method != http.MethodDelete {
		return fmt.Errorf("workspace/sandboxed: unsupported loopback method %q", request.Method)
	}
	for name, values := range request.Header {
		if !strings.EqualFold(name, "Content-Type") ||
			len(values) != 1 ||
			values[0] != "application/json" {
			return fmt.Errorf("workspace/sandboxed: loopback request contains unsupported headers")
		}
	}
	if request.MaxResponseBytes <= 0 {
		return fmt.Errorf("workspace/sandboxed: loopback response limit must be positive")
	}
	return nil
}

func (t *pythonLoopbackTransport) decodeResponse(
	ctx context.Context,
	data []byte,
	maxResponseBytes int64,
) (*gateway.Response, error) {
	return gateway.DecodeLoopbackResponse(
		ctx,
		data,
		maxResponseBytes,
		func(ctx context.Context, filename string, maxBytes int64) ([]byte, error) {
			defer func() {
				cleanupCtx, cancel := t.detachedContext(ctx)
				defer cancel()
				t.deleteTempFile(cleanupCtx, filename)
			}()
			if reader, ok := t.backend.(LimitedFileReader); ok {
				return reader.ReadFileLimit(ctx, filename, maxBytes)
			}
			return t.backend.ReadFile(ctx, filename)
		},
	)
}

func (t *pythonLoopbackTransport) detachedContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := defaultGatewayTimeout
	if t != nil && t.timeout > 0 {
		timeout = min(t.timeout, defaultGatewayTimeout)
	}
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

func (t *pythonLoopbackTransport) deleteTempFile(ctx context.Context, filename string) {
	if filename == "" || !strings.HasPrefix(filename, loopbackTempDir+"/") {
		return
	}
	_, _ = t.backend.Exec(ctx, []string{"unlink", "--", filename}, ExecOptions{CWD: "/"})
}

var (
	_ gateway.Transport = (*pythonLoopbackTransport)(nil)
)
