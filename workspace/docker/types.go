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

// Package docker provides a Docker-backed workspace implementation.
package docker

import (
	"context"
	"time"
)

const (
	defaultImage            = "ubuntu:latest"
	defaultContainerWorkdir = "/workspace"
	defaultStopTimeout      = 5 * time.Second
	defaultBashTimeout      = 120 * time.Second
	maxBashTimeout          = 10 * time.Minute
	defaultFileMode         = 0o644
)

type containerSpec struct {
	ID               string
	Image            string
	Name             string
	Workdir          string
	HostWorkdir      string
	Env              map[string]string
	KeepContainer    bool
	PullImage        bool
	StopTimeout      time.Duration
	NetworkDisabled  bool
	MemoryBytes      int64
	NanoCPUs         int64
	ExtraLabels      map[string]string
	RemoveOnClose    bool
	ContainerCommand []string
}

type runRequest struct {
	Command string
	Stdin   []byte
	Workdir string
	Env     map[string]string
	Timeout time.Duration
}

type runResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type runtime interface {
	Create(context.Context, containerSpec) (string, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Remove(context.Context, string) error
	Run(context.Context, string, runRequest) (runResult, error)
	ReadFile(context.Context, string, string) ([]byte, error)
	WriteFile(context.Context, string, string, []byte, int64) error
	Close() error
}
