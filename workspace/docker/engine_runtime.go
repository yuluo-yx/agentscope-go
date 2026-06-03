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
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	mobyclient "github.com/moby/moby/client"
)

type engineRuntime struct {
	client *mobyclient.Client
}

func newEngineRuntime(ctx context.Context) (*engineRuntime, error) {
	apiClient, err := mobyclient.New(
		mobyclient.FromEnv,
		mobyclient.WithUserAgent("agentscope-go/workspace-docker"),
	)
	if err != nil {
		return nil, err
	}
	if _, err := apiClient.Ping(ctx, mobyclient.PingOptions{}); err != nil {
		_ = apiClient.Close()
		return nil, err
	}
	return &engineRuntime{client: apiClient}, nil
}

func (r *engineRuntime) Create(ctx context.Context, spec containerSpec) (string, error) {
	if spec.PullImage {
		if err := r.pullImage(ctx, spec.Image); err != nil {
			return "", err
		}
	}
	config := &container.Config{
		Image:           spec.Image,
		WorkingDir:      spec.Workdir,
		Cmd:             spec.ContainerCommand,
		Env:             envList(spec.Env),
		Labels:          labels(spec),
		NetworkDisabled: spec.NetworkDisabled,
		StopTimeout:     durationSecondsPtr(spec.StopTimeout),
	}
	hostConfig := &container.HostConfig{
		AutoRemove: false,
		Resources: container.Resources{
			Memory:   spec.MemoryBytes,
			NanoCPUs: spec.NanoCPUs,
		},
	}
	if spec.NetworkDisabled {
		hostConfig.NetworkMode = container.NetworkMode("none")
	}
	if spec.HostWorkdir != "" {
		hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: spec.HostWorkdir,
			Target: spec.Workdir,
		})
	}
	created, err := r.client.ContainerCreate(ctx, mobyclient.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
		Name:       spec.Name,
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func (r *engineRuntime) Start(ctx context.Context, containerID string) error {
	_, err := r.client.ContainerStart(ctx, containerID, mobyclient.ContainerStartOptions{})
	return err
}

func (r *engineRuntime) Stop(ctx context.Context, containerID string) error {
	_, err := r.client.ContainerStop(ctx, containerID, mobyclient.ContainerStopOptions{})
	return err
}

func (r *engineRuntime) Remove(ctx context.Context, containerID string) error {
	_, err := r.client.ContainerRemove(ctx, containerID, mobyclient.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	})
	return err
}

func (r *engineRuntime) Run(ctx context.Context, containerID string, req runRequest) (runResult, error) {
	execCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()

	exec, err := r.client.ExecCreate(execCtx, containerID, mobyclient.ExecCreateOptions{
		AttachStdin:  len(req.Stdin) > 0,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          false,
		WorkingDir:   req.Workdir,
		Env:          envList(req.Env),
		Cmd:          []string{"/bin/bash", "-lc", req.Command},
	})
	if err != nil {
		return runResult{}, err
	}
	attached, err := r.client.ExecAttach(execCtx, exec.ID, mobyclient.ExecAttachOptions{TTY: false})
	if err != nil {
		return runResult{}, err
	}
	defer attached.Close()

	if len(req.Stdin) > 0 {
		if _, writeErr := attached.Conn.Write(req.Stdin); writeErr != nil {
			return runResult{}, writeErr
		}
		if closeErr := attached.CloseWrite(); closeErr != nil {
			return runResult{}, closeErr
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, copyErr := stdcopy.StdCopy(&stdout, &stderr, attached.Reader); copyErr != nil {
		return runResult{}, copyErr
	}
	inspect, err := r.client.ExecInspect(ctx, exec.ID, mobyclient.ExecInspectOptions{})
	if err != nil {
		return runResult{}, err
	}
	return runResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: inspect.ExitCode}, nil
}

func (r *engineRuntime) ReadFile(ctx context.Context, containerID, filePath string) ([]byte, error) {
	copied, err := r.client.CopyFromContainer(ctx, containerID, mobyclient.CopyFromContainerOptions{SourcePath: filePath})
	if err != nil {
		return nil, err
	}
	defer copied.Content.Close()
	reader := tar.NewReader(copied.Content)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.FileInfo().IsDir() {
			continue
		}
		return io.ReadAll(reader)
	}
	return nil, fmt.Errorf("path is a directory or empty archive: %s", filePath)
}

func (r *engineRuntime) WriteFile(ctx context.Context, containerID, filePath string, data []byte, mode int64) error {
	archive, err := tarFile(filePath, data, mode)
	if err != nil {
		return err
	}
	_, err = r.client.CopyToContainer(ctx, containerID, mobyclient.CopyToContainerOptions{
		DestinationPath:           "/",
		Content:                   archive,
		AllowOverwriteDirWithFile: true,
	})
	return err
}

func (r *engineRuntime) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *engineRuntime) pullImage(ctx context.Context, image string) error {
	pulled, err := r.client.ImagePull(ctx, image, mobyclient.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer pulled.Close()
	return pulled.Wait(ctx)
}

func tarFile(filePath string, data []byte, mode int64) (io.Reader, error) {
	cleaned := strings.TrimPrefix(path.Clean(filePath), "/")
	if cleaned == "." || cleaned == "" {
		return nil, fmt.Errorf("invalid file path: %s", filePath)
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	parts := strings.Split(path.Dir(cleaned), "/")
	dir := ""
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		dir = path.Join(dir, part)
		if err := writer.WriteHeader(&tar.Header{Name: dir, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			return nil, err
		}
	}
	if err := writer.WriteHeader(&tar.Header{
		Name:     cleaned,
		Mode:     mode,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return bytes.NewReader(buffer.Bytes()), nil
}

func envList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	values := make([]string, 0, len(env))
	for key, value := range env {
		values = append(values, key+"="+value)
	}
	return values
}

func durationSecondsPtr(duration time.Duration) *int {
	if duration <= 0 {
		return nil
	}
	seconds := int(duration / time.Second)
	if seconds == 0 {
		seconds = 1
	}
	return &seconds
}

func labels(spec containerSpec) map[string]string {
	out := map[string]string{
		"agentscope-go.workspace.id": spec.ID,
		"agentscope-go.workspace":    "docker",
	}
	for key, value := range spec.ExtraLabels {
		out[key] = value
	}
	return out
}
