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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"time"

	sdk "github.com/alibaba/OpenSandbox/sdks/sandbox/go"
)

type sdkRuntime struct{}

func (*sdkRuntime) List(
	ctx context.Context,
	connection sdk.ConnectionConfig,
	workspaceID string,
) ([]sandboxInfo, error) {
	manager := sdk.NewSandboxManager(connection)
	defer manager.Close()
	page := 1
	infos := []sandboxInfo{}
	for {
		response, err := manager.ListSandboxInfos(ctx, sdk.ListOptions{
			States:   []sdk.SandboxState{sdk.StateRunning, sdk.StatePaused},
			Metadata: map[string]string{metadataWorkspaceID: workspaceID},
			Page:     page,
			PageSize: 100,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range response.Items {
			infos = append(infos, sandboxInfo{
				ID:        item.ID,
				State:     item.Status.State,
				CreatedAt: item.CreatedAt,
			})
		}
		if !response.Pagination.HasNextPage {
			break
		}
		page++
	}
	return infos, nil
}

func (*sdkRuntime) Create(ctx context.Context, spec sandboxSpec) (sandboxHandle, error) {
	timeoutSeconds, err := durationSeconds(spec.Timeout)
	if err != nil {
		return nil, err
	}
	sandbox, err := sdk.CreateSandbox(ctx, spec.Connection, sdk.SandboxCreateOptions{
		Image:               spec.Image,
		Entrypoint:          append([]string(nil), spec.Entrypoint...),
		ResourceLimits:      cloneResourceLimits(spec.ResourceLimits),
		TimeoutSeconds:      &timeoutSeconds,
		Env:                 cloneStringMap(spec.Env),
		Metadata:            cloneStringMap(spec.Metadata),
		NetworkPolicy:       cloneNetworkPolicy(spec.NetworkPolicy),
		ReadyTimeout:        spec.Timeout,
		HealthCheckInterval: 500 * time.Millisecond,
	})
	if err != nil {
		return nil, err
	}
	return &sdkHandle{sandbox: sandbox}, nil
}

func (*sdkRuntime) Connect(
	ctx context.Context,
	connection sdk.ConnectionConfig,
	sandboxID string,
	timeout time.Duration,
) (sandboxHandle, error) {
	sandbox, err := sdk.ConnectSandbox(ctx, connection, sandboxID, sdk.ReadyOptions{
		Timeout:         timeout,
		PollingInterval: 500 * time.Millisecond,
	})
	if err != nil {
		return nil, err
	}
	return &sdkHandle{sandbox: sandbox}, nil
}

func (*sdkRuntime) Resume(
	ctx context.Context,
	connection sdk.ConnectionConfig,
	sandboxID string,
	timeout time.Duration,
) (sandboxHandle, error) {
	sandbox, err := sdk.ResumeSandbox(ctx, connection, sandboxID, sdk.ReadyOptions{
		Timeout:         timeout,
		PollingInterval: 500 * time.Millisecond,
	})
	if err != nil {
		return nil, err
	}
	return &sdkHandle{sandbox: sandbox}, nil
}

type sdkHandle struct {
	sandbox *sdk.Sandbox
}

func (h *sdkHandle) ID() string {
	if h == nil || h.sandbox == nil {
		return ""
	}
	return h.sandbox.ID()
}

func (h *sdkHandle) Healthy(ctx context.Context) (bool, error) {
	if h == nil || h.sandbox == nil {
		return false, fmt.Errorf("workspace/opensandbox: nil SDK sandbox")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return h.sandbox.IsHealthy(ctx), nil
}

func (h *sdkHandle) Run(
	ctx context.Context,
	argv []string,
	cwd string,
	env map[string]string,
	timeout time.Duration,
) (runResult, error) {
	if h == nil || h.sandbox == nil {
		return runResult{}, fmt.Errorf("workspace/opensandbox: nil SDK sandbox")
	}
	command := shellJoin(argv)
	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	request := sdk.RunCommandRequest{
		Command: command,
		Cwd:     cwd,
		Envs:    cloneStringMap(env),
	}
	if timeout > 0 {
		milliseconds := timeout.Milliseconds()
		if milliseconds == 0 {
			milliseconds = 1
		}
		request.Timeout = milliseconds
	}
	execution, err := h.sandbox.RunCommandWithOpts(runCtx, request, nil)
	if err != nil {
		return runResult{}, err
	}
	if execution == nil {
		return runResult{}, fmt.Errorf("workspace/opensandbox: SDK returned nil execution")
	}
	exitCode := 0
	if execution.ExitCode != nil {
		exitCode = *execution.ExitCode
	} else if execution.Error != nil {
		exitCode = -1
	}
	stdout := joinOutputMessages(execution.Stdout)
	stderr := joinOutputMessages(execution.Stderr)
	if execution.Error != nil {
		detail := strings.TrimSpace(execution.Error.Name + ": " + execution.Error.Value)
		if detail != ":" && !strings.Contains(stderr, detail) {
			if stderr != "" && !strings.HasSuffix(stderr, "\n") {
				stderr += "\n"
			}
			stderr += detail
		}
	}
	return runResult{
		ExitCode: exitCode,
		Stdout:   []byte(stdout),
		Stderr:   []byte(stderr),
	}, nil
}

func (h *sdkHandle) ReadFile(ctx context.Context, filename string) ([]byte, error) {
	if h == nil || h.sandbox == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil SDK sandbox")
	}
	reader, err := h.sandbox.DownloadFile(ctx, filename, "")
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	return data, nil
}

func (h *sdkHandle) ReadFileLimit(ctx context.Context, filename string, maxBytes int64) ([]byte, error) {
	if h == nil || h.sandbox == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil SDK sandbox")
	}
	if maxBytes <= 0 || maxBytes >= math.MaxInt64 {
		return nil, fmt.Errorf("workspace/opensandbox: remote file limit must be positive")
	}
	reader, err := h.sandbox.DownloadFile(ctx, filename, "")
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("workspace/opensandbox: remote file exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func (h *sdkHandle) WriteFile(ctx context.Context, filename string, data []byte) error {
	if h == nil || h.sandbox == nil {
		return fmt.Errorf("workspace/opensandbox: nil SDK sandbox")
	}
	parent := path.Dir(filename)
	if parent != "." && parent != "/" {
		if err := h.sandbox.CreateDirectory(ctx, parent, 755); err != nil {
			return err
		}
	}
	return h.sandbox.UploadFile(ctx, bytes.NewReader(data), sdk.UploadFileOptions{
		FileName: path.Base(filename),
		Metadata: sdk.FileMetadata{
			Path: filename,
			Mode: 644,
		},
	})
}

func (h *sdkHandle) Pause(ctx context.Context) error {
	if h == nil || h.sandbox == nil {
		return nil
	}
	return h.sandbox.Pause(ctx)
}

func (h *sdkHandle) Close() error {
	if h == nil || h.sandbox == nil {
		return nil
	}
	return h.sandbox.Close()
}

func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for index, argument := range argv {
		quoted[index] = shellQuote(argument)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func joinOutputMessages(messages []sdk.OutputMessage) string {
	var builder strings.Builder
	wroteText := false
	endsWithNewline := false
	for _, message := range messages {
		text := message.Text
		if wroteText && text != "" {
			if !endsWithNewline && !strings.HasPrefix(text, "\n") {
				builder.WriteByte('\n')
			}
		}
		builder.WriteString(text)
		if text != "" {
			wroteText = true
			endsWithNewline = strings.HasSuffix(text, "\n")
		}
	}
	return builder.String()
}

func durationSeconds(duration time.Duration) (int, error) {
	if duration <= 0 {
		return 0, fmt.Errorf("workspace/opensandbox: timeout must be positive")
	}
	seconds := int64(math.Ceil(duration.Seconds()))
	if seconds > int64(math.MaxInt) {
		return 0, fmt.Errorf("workspace/opensandbox: timeout exceeds int range")
	}
	return int(seconds), nil
}

var (
	_ sandboxRuntime = (*sdkRuntime)(nil)
	_ sandboxHandle  = (*sdkHandle)(nil)
)
