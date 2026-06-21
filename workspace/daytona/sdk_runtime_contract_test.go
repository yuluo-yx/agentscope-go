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
	"strings"
	"testing"
	"time"

	sdktypes "github.com/daytonaio/daytona/libs/sdk-go/pkg/types"
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
	if _, err := nilRuntime.Get(context.Background(), sandboxSpec{}, "sandbox-1"); err == nil || !strings.Contains(err.Error(), "nil SDK runtime") {
		t.Fatalf("nil Get error = %v, want nil SDK runtime", err)
	}
	if err := nilRuntime.Close(); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := (&sdkRuntime{}).Close(); err != nil {
		t.Fatalf("empty Close returned error: %v", err)
	}
}

func TestCreateParamsFromSpecSelectsImageSnapshotAndResources(t *testing.T) {
	t.Parallel()

	imageParams, ok := createParamsFromSpec(sandboxSpec{
		ID:     "workspace-1",
		Image:  "python:3.12",
		Env:    map[string]string{"DATASET": "sales"},
		CPU:    2,
		GPU:    1,
		Memory: 4096,
		Disk:   20,
	}).(sdktypes.ImageParams)
	if !ok {
		t.Fatalf("createParamsFromSpec image type = %T", imageParams)
	}
	if imageParams.Name != "workspace-1" || imageParams.Image != "python:3.12" ||
		imageParams.EnvVars["DATASET"] != "sales" || imageParams.Labels["agentscope-workspace-id"] != "workspace-1" {
		t.Fatalf("image params mismatch: %#v", imageParams)
	}
	if imageParams.Resources == nil || imageParams.Resources.CPU != 2 || imageParams.Resources.GPU != 1 ||
		imageParams.Resources.Memory != 4096 || imageParams.Resources.Disk != 20 {
		t.Fatalf("resources mismatch: %#v", imageParams.Resources)
	}
	imageParams.EnvVars["DATASET"] = "mutated"
	if got := createParamsFromSpec(sandboxSpec{ID: "workspace-1", Env: map[string]string{"DATASET": "sales"}}).(sdktypes.ImageParams).EnvVars["DATASET"]; got != "sales" {
		t.Fatalf("env vars should be cloned, got %q", got)
	}

	defaultImageParams := createParamsFromSpec(sandboxSpec{ID: "workspace-default"}).(sdktypes.ImageParams)
	if defaultImageParams.Image != defaultImage || defaultImageParams.Resources != nil {
		t.Fatalf("default image params mismatch: %#v", defaultImageParams)
	}

	snapshotParams, ok := createParamsFromSpec(sandboxSpec{ID: "workspace-2", Snapshot: "snap-1"}).(sdktypes.SnapshotParams)
	if !ok {
		t.Fatalf("createParamsFromSpec snapshot type = %T", snapshotParams)
	}
	if snapshotParams.Name != "workspace-2" || snapshotParams.Snapshot != "snap-1" {
		t.Fatalf("snapshot params mismatch: %#v", snapshotParams)
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
	if err := nilHandle.Delete(context.Background()); err != nil {
		t.Fatalf("nil Delete returned error: %v", err)
	}
	if err := nilHandle.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect returned error: %v", err)
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
