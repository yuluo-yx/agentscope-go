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
	"strings"
	"testing"
	"time"
)

func TestSDKRuntimeRejectsNilReceiverAndClosesIdempotently(t *testing.T) {
	t.Parallel()

	runtime, err := newSDKRuntime()
	if err != nil {
		t.Fatalf("newSDKRuntime returned error: %v", err)
	}
	if runtime == nil {
		t.Fatalf("newSDKRuntime returned nil runtime")
	}

	var nilRuntime *sdkRuntime
	if _, err := nilRuntime.Create(context.Background(), sandboxSpec{}); err == nil || !strings.Contains(err.Error(), "nil SDK runtime") {
		t.Fatalf("nil Create error = %v, want nil SDK runtime", err)
	}
	if err := nilRuntime.Close(); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := (&sdkRuntime{}).Close(); err != nil {
		t.Fatalf("empty Close returned error: %v", err)
	}
}

func TestSDKHandleNilBranchesAndDefaults(t *testing.T) {
	t.Parallel()

	var nilHandle *sdkHandle
	if nilHandle.ID() != "" {
		t.Fatalf("nil handle ID should be empty")
	}
	if ready, err := nilHandle.IsReady(context.Background()); err != nil || ready {
		t.Fatalf("nil IsReady = %v, %v; want false nil", ready, err)
	}
	if _, err := nilHandle.Run(context.Background(), runRequest{}); err == nil || !strings.Contains(err.Error(), "nil SDK sandbox handle") {
		t.Fatalf("nil Run error = %v, want nil SDK sandbox handle", err)
	}
	if _, err := nilHandle.Read(context.Background(), "/tmp/file"); err == nil || !strings.Contains(err.Error(), "nil SDK sandbox handle") {
		t.Fatalf("nil Read error = %v, want nil SDK sandbox handle", err)
	}
	if err := nilHandle.Write(context.Background(), "/tmp/file", []byte("data")); err == nil || !strings.Contains(err.Error(), "nil SDK sandbox handle") {
		t.Fatalf("nil Write error = %v, want nil SDK sandbox handle", err)
	}
	if err := nilHandle.Stop(context.Background()); err != nil {
		t.Fatalf("nil Stop returned error: %v", err)
	}
	if err := nilHandle.Detach(context.Background()); err != nil {
		t.Fatalf("nil Detach returned error: %v", err)
	}
	if err := nilHandle.Close(); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := nilHandle.ensureWorkdir(context.Background()); err == nil || !strings.Contains(err.Error(), "nil SDK sandbox handle") {
		t.Fatalf("nil ensureWorkdir error = %v, want nil SDK sandbox handle", err)
	}
	if nilHandle.workdir() != defaultContainerWorkdir || nilHandle.requestTimeout() != defaultRequestTimeout {
		t.Fatalf("nil handle defaults mismatch")
	}

	handle := &sdkHandle{spec: sandboxSpec{Workdir: "/workspace/app", RequestTimeout: 3 * time.Second}}
	if handle.workdir() != "/workspace/app" || handle.requestTimeout() != 3*time.Second {
		t.Fatalf("handle defaults mismatch: workdir=%q timeout=%s", handle.workdir(), handle.requestTimeout())
	}
	if filepathDir("/workspace/app/file.txt") != "/workspace/app" ||
		filepathDir("relative.txt") != "/" ||
		filepathDir("/file.txt") != "/" {
		t.Fatalf("filepathDir returned unexpected parent paths")
	}
}

func TestMicrosandboxSpecDefaultHelpers(t *testing.T) {
	t.Parallel()

	spec := sandboxSpec{ID: "workspace-1"}
	if nameFromSpec(spec) != "workspace-1" ||
		imageFromSpec(spec) != defaultImage ||
		workdirFromSpec(spec) != defaultContainerWorkdir {
		t.Fatalf("default helper mismatch for id-only spec")
	}
	spec = sandboxSpec{Name: "named", Image: "python:3.12", Workdir: "/app"}
	if nameFromSpec(spec) != "named" || imageFromSpec(spec) != "python:3.12" || workdirFromSpec(spec) != "/app" {
		t.Fatalf("explicit helper mismatch")
	}
	if nameFromSpec(sandboxSpec{}) != "agentscope-microsandbox" {
		t.Fatalf("empty spec should use stable fallback name")
	}

	ctx, cancel := contextWithOptionalTimeout(context.Background(), 0)
	cancel()
	if ctx.Err() != nil {
		t.Fatalf("zero timeout should return original uncanceled context")
	}
	timeoutCtx, timeoutCancel := contextWithOptionalTimeout(context.Background(), time.Nanosecond)
	defer timeoutCancel()
	select {
	case <-timeoutCtx.Done():
	case <-time.After(time.Second):
		t.Fatalf("positive timeout context did not expire")
	}
}
