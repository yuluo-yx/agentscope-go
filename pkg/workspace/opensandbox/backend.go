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
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/workspace/internal/sandboxed"
)

type backend struct {
	handle  sandboxHandle
	workdir string
}

func (b *backend) Exec(
	ctx context.Context,
	argv []string,
	options sandboxed.ExecOptions,
) (sandboxed.ExecResult, error) {
	if b == nil || b.handle == nil {
		return sandboxed.ExecResult{}, fmt.Errorf("workspace/opensandbox: nil sandbox backend")
	}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return sandboxed.ExecResult{}, fmt.Errorf("workspace/opensandbox: command argv is empty")
	}
	for _, argument := range argv {
		if strings.ContainsRune(argument, '\x00') {
			return sandboxed.ExecResult{}, fmt.Errorf("workspace/opensandbox: command argv contains NUL byte")
		}
	}
	cwd := options.CWD
	if cwd == "" {
		cwd = b.workdir
	}
	if !strings.HasPrefix(cwd, "/") || strings.ContainsRune(cwd, '\x00') {
		return sandboxed.ExecResult{}, fmt.Errorf("workspace/opensandbox: command cwd must be absolute")
	}
	result, err := b.handle.Run(ctx, argv, cwd, cloneStringMap(options.Env), options.Timeout)
	if err != nil {
		return sandboxed.ExecResult{}, err
	}
	return sandboxed.ExecResult{
		ExitCode: result.ExitCode,
		Stdout:   append([]byte(nil), result.Stdout...),
		Stderr:   append([]byte(nil), result.Stderr...),
	}, nil
}

func (b *backend) ReadFile(ctx context.Context, filename string) ([]byte, error) {
	if b == nil || b.handle == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil sandbox backend")
	}
	return b.handle.ReadFile(ctx, filename)
}

func (b *backend) ReadFileLimit(ctx context.Context, filename string, maxBytes int64) ([]byte, error) {
	if b == nil || b.handle == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil sandbox backend")
	}
	if reader, ok := b.handle.(interface {
		ReadFileLimit(context.Context, string, int64) ([]byte, error)
	}); ok {
		return reader.ReadFileLimit(ctx, filename, maxBytes)
	}
	data, err := b.handle.ReadFile(ctx, filename)
	if err != nil {
		return nil, err
	}
	if maxBytes <= 0 || int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("workspace/opensandbox: remote file exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func (b *backend) WriteFile(ctx context.Context, filename string, data []byte) error {
	if b == nil || b.handle == nil {
		return fmt.Errorf("workspace/opensandbox: nil sandbox backend")
	}
	return b.handle.WriteFile(ctx, filename, data)
}

var _ sandboxed.Backend = (*backend)(nil)
var _ sandboxed.LimitedFileReader = (*backend)(nil)
