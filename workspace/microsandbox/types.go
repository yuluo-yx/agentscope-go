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
	"time"
)

const (
	defaultContainerWorkdir = "/workspace"
	defaultImage            = "python:3.12"
	defaultRequestTimeout   = 60 * time.Second
	defaultOpenTimeout      = 2 * time.Minute
	defaultBashTimeout      = 120 * time.Second
	maxBashTimeout          = 10 * time.Minute
	maxReadLineCharacters   = 2000
)

type sandboxSpec struct {
	ID              string
	Name            string
	Image           string
	Workdir         string
	Env             map[string]string
	CPUs            uint8
	MemoryMiB       uint32
	EnsureInstalled bool
	RequestTimeout  time.Duration
	OpenTimeout     time.Duration
}

type runRequest struct {
	Command string
	Workdir string
	Env     map[string]string
	Timeout time.Duration
}

type runResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type sandboxRuntime interface {
	Create(context.Context, sandboxSpec) (sandboxHandle, error)
	Close() error
}

type sandboxHandle interface {
	ID() string
	IsReady(context.Context) (bool, error)
	Run(context.Context, runRequest) (runResult, error)
	Read(context.Context, string) ([]byte, error)
	Write(context.Context, string, []byte) error
	Stop(context.Context) error
	Detach(context.Context) error
	Close() error
}
