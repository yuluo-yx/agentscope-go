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
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

type execCall struct {
	argv    []string
	options ExecOptions
}

type execHook func(context.Context, []string, ExecOptions) (ExecResult, error, bool)
type readHook func(context.Context, string) ([]byte, error, bool)
type writeHook func(context.Context, string, []byte) (error, bool)

type fakeBackend struct {
	mu sync.Mutex

	files map[string][]byte
	dirs  map[string]bool

	execHook  execHook
	readHook  readHook
	writeHook writeHook

	execCalls  []execCall
	readCalls  []string
	writeCalls []string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		files: map[string][]byte{},
		dirs:  map[string]bool{"/": true},
	}
}

func (b *fakeBackend) Exec(ctx context.Context, argv []string, options ExecOptions) (ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return ExecResult{}, err
	}
	b.mu.Lock()
	b.execCalls = append(b.execCalls, execCall{
		argv: append([]string(nil), argv...),
		options: ExecOptions{
			CWD:     options.CWD,
			Env:     cloneStringMap(options.Env),
			Timeout: options.Timeout,
		},
	})
	hook := b.execHook
	b.mu.Unlock()
	if hook != nil {
		if result, err, handled := hook(ctx, argv, options); handled {
			return result, err
		}
	}
	if len(argv) == 0 {
		return ExecResult{ExitCode: 127, Stderr: []byte("empty command")}, nil
	}

	switch argv[0] {
	case "test":
		if len(argv) != 3 {
			return ExecResult{ExitCode: 2}, nil
		}
		b.mu.Lock()
		exists := b.existsLocked(argv[2])
		if argv[1] == "-f" {
			_, exists = b.files[path.Clean(argv[2])]
		}
		b.mu.Unlock()
		if exists {
			return ExecResult{ExitCode: 0}, nil
		}
		return ExecResult{ExitCode: 1}, nil
	case "mkdir":
		b.mu.Lock()
		for _, dir := range argv[1:] {
			if dir != "-p" {
				b.dirs[path.Clean(dir)] = true
			}
		}
		b.mu.Unlock()
		return ExecResult{ExitCode: 0}, nil
	case "find":
		return b.find(argv), nil
	case "grep":
		return b.grep(argv), nil
	case "unlink":
		if len(argv) == 3 && argv[1] == "--" {
			b.mu.Lock()
			delete(b.files, path.Clean(argv[2]))
			b.mu.Unlock()
			return ExecResult{ExitCode: 0}, nil
		}
	case "sh":
		if len(argv) >= 3 && argv[1] == "-c" && argv[2] == deleteTreeScript {
			b.mu.Lock()
			for _, target := range argv[4:] {
				b.deleteTreeLocked(target)
			}
			b.mu.Unlock()
			return ExecResult{ExitCode: 0}, nil
		}
		if len(argv) >= 3 && argv[1] == "-c" && argv[2] == launchGatewayScript {
			return ExecResult{ExitCode: 0}, nil
		}
	case "/bin/bash":
		return ExecResult{ExitCode: 0, Stdout: []byte("ok\n")}, nil
	}

	return ExecResult{ExitCode: 0}, nil
}

func (b *fakeBackend) ReadFile(ctx context.Context, filename string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.readCalls = append(b.readCalls, filename)
	hook := b.readHook
	b.mu.Unlock()
	if hook != nil {
		if data, err, handled := hook(ctx, filename); handled {
			return bytes.Clone(data), err
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	data, exists := b.files[path.Clean(filename)]
	if !exists {
		return nil, fmt.Errorf("fake backend: %s not found", filename)
	}
	return bytes.Clone(data), nil
}

func (b *fakeBackend) WriteFile(ctx context.Context, filename string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	b.writeCalls = append(b.writeCalls, filename)
	hook := b.writeHook
	b.mu.Unlock()
	if hook != nil {
		if err, handled := hook(ctx, filename, data); handled {
			return err
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	filename = path.Clean(filename)
	b.files[filename] = bytes.Clone(data)
	for dir := path.Dir(filename); dir != "." && dir != "/"; dir = path.Dir(dir) {
		b.dirs[dir] = true
	}
	return nil
}

func (b *fakeBackend) find(argv []string) ExecResult {
	if len(argv) < 2 {
		return ExecResult{ExitCode: 2}
	}
	root := path.Clean(argv[1])
	wantSkill := false
	for index := range argv {
		if argv[index] == "-name" && index+1 < len(argv) && argv[index+1] == "SKILL.md" {
			wantSkill = true
		}
	}
	b.mu.Lock()
	files := make([]string, 0, len(b.files))
	for filename := range b.files {
		if !insideRemoteDir(root, filename) || wantSkill && path.Base(filename) != "SKILL.md" {
			continue
		}
		files = append(files, filename)
	}
	b.mu.Unlock()
	sort.Strings(files)
	if len(files) == 0 {
		return ExecResult{ExitCode: 0}
	}
	return ExecResult{ExitCode: 0, Stdout: []byte(strings.Join(files, "\n") + "\n")}
}

func (b *fakeBackend) grep(argv []string) ExecResult {
	if len(argv) < 3 {
		return ExecResult{ExitCode: 2, Stderr: []byte("bad grep argv")}
	}
	caseInsensitive := false
	separator := -1
	for index, arg := range argv {
		if arg == "-i" {
			caseInsensitive = true
		}
		if arg == "--" {
			separator = index
		}
	}
	if separator < 0 || separator+2 >= len(argv) {
		return ExecResult{ExitCode: 2, Stderr: []byte("bad grep argv")}
	}
	pattern := argv[separator+1]
	if caseInsensitive {
		pattern = "(?i)" + pattern
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return ExecResult{ExitCode: 2, Stderr: []byte(err.Error())}
	}
	root := path.Clean(argv[separator+2])
	b.mu.Lock()
	filenames := make([]string, 0, len(b.files))
	for filename := range b.files {
		if insideRemoteDir(root, filename) {
			filenames = append(filenames, filename)
		}
	}
	sort.Strings(filenames)
	var matches []string
	for _, filename := range filenames {
		for index, line := range strings.Split(string(b.files[filename]), "\n") {
			if expression.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", filename, index+1, line))
			}
		}
	}
	b.mu.Unlock()
	if len(matches) == 0 {
		return ExecResult{ExitCode: 1}
	}
	return ExecResult{ExitCode: 0, Stdout: []byte(strings.Join(matches, "\n") + "\n")}
}

func (b *fakeBackend) existsLocked(filename string) bool {
	filename = path.Clean(filename)
	if _, exists := b.files[filename]; exists {
		return true
	}
	if b.dirs[filename] {
		return true
	}
	for current := range b.files {
		if insideRemoteDir(filename, current) {
			return true
		}
	}
	return false
}

func (b *fakeBackend) deleteTreeLocked(target string) {
	target = path.Clean(target)
	for filename := range b.files {
		if insideRemoteDir(target, filename) {
			delete(b.files, filename)
		}
	}
	for dir := range b.dirs {
		if dir != "/" && insideRemoteDir(target, dir) {
			delete(b.dirs, dir)
		}
	}
}

func (b *fakeBackend) file(filename string) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.files[path.Clean(filename)])
}

func (b *fakeBackend) callsFor(command string) []execCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	var calls []execCall
	for _, call := range b.execCalls {
		if len(call.argv) > 0 && call.argv[0] == command {
			calls = append(calls, call)
		}
	}
	return calls
}

type fakeProvider struct {
	backend  Backend
	openErr  error
	closeErr error

	mu         sync.Mutex
	openCalls  int
	closeCalls int
}

func (p *fakeProvider) Open(ctx context.Context) (Backend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.openCalls++
	p.mu.Unlock()
	return p.backend, p.openErr
}

func (p *fakeProvider) Close(context.Context) error {
	p.mu.Lock()
	p.closeCalls++
	p.mu.Unlock()
	return p.closeErr
}

func (p *fakeProvider) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.openCalls, p.closeCalls
}

type fakeGateway struct {
	mu sync.Mutex

	configs []workspace.MCPClientConfig

	bootstrapErrors []error
	removeErrors    []error
	listErr         error
	addErr          error
	removeErr       error
	closeErr        error

	bootstrapCalls int
	listCalls      int
	addCalls       []workspace.MCPClientConfig
	removeCalls    []string
	closeCalls     int
	createdClients []*fakeMCP
}

func (g *fakeGateway) Bootstrap(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bootstrapCalls++
	if len(g.bootstrapErrors) == 0 {
		return nil
	}
	err := g.bootstrapErrors[0]
	g.bootstrapErrors = g.bootstrapErrors[1:]
	return err
}

func (g *fakeGateway) AddMCP(ctx context.Context, config workspace.MCPClientConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.addCalls = append(g.addCalls, cloneMCPClientConfig(config))
	if g.addErr != nil {
		return g.addErr
	}
	g.configs = append(g.configs, cloneMCPClientConfig(config))
	return nil
}

func (g *fakeGateway) RemoveMCP(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.removeCalls = append(g.removeCalls, name)
	if len(g.removeErrors) > 0 {
		err := g.removeErrors[0]
		g.removeErrors = g.removeErrors[1:]
		if err != nil {
			return err
		}
	}
	if g.removeErr != nil {
		return g.removeErr
	}
	for index, config := range g.configs {
		if config.Name == name {
			g.configs = append(g.configs[:index], g.configs[index+1:]...)
			break
		}
	}
	return nil
}

func (g *fakeGateway) ListMCPs(ctx context.Context) ([]workspace.MCPClientConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.listCalls++
	if g.listErr != nil {
		return nil, g.listErr
	}
	configs := make([]workspace.MCPClientConfig, 0, len(g.configs))
	for _, config := range g.configs {
		configs = append(configs, cloneMCPClientConfig(config))
	}
	return configs, nil
}

func (g *fakeGateway) NewMCPClient(config workspace.MCPClientConfig, connected bool) workspace.MCPClient {
	g.mu.Lock()
	defer g.mu.Unlock()
	client := &fakeMCP{config: cloneMCPClientConfig(config), connected: connected}
	g.createdClients = append(g.createdClients, client)
	return client
}

func (g *fakeGateway) Close(context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closeCalls++
	return g.closeErr
}

type fakeCodec struct {
	marshalErr    error
	unmarshalErr  error
	marshalHook   func([]workspace.MCPClientConfig) ([]byte, error)
	unmarshalHook func([]byte) ([]workspace.MCPClientConfig, error)
}

func (c *fakeCodec) Marshal(configs []workspace.MCPClientConfig) ([]byte, error) {
	if c.marshalHook != nil {
		return c.marshalHook(configs)
	}
	if c.marshalErr != nil {
		return nil, c.marshalErr
	}
	return json.Marshal(configs)
}

func (c *fakeCodec) Unmarshal(data []byte) ([]workspace.MCPClientConfig, error) {
	if c.unmarshalHook != nil {
		return c.unmarshalHook(data)
	}
	if c.unmarshalErr != nil {
		return nil, c.unmarshalErr
	}
	var configs []workspace.MCPClientConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

type fakeMCP struct {
	mu        sync.RWMutex
	name      string
	config    workspace.MCPClientConfig
	configErr error
	connected bool
}

func (m *fakeMCP) Name() string {
	if m.name != "" {
		return m.name
	}
	return m.config.Name
}
func (m *fakeMCP) IsStateful() bool { return m.config.Stateful }
func (m *fakeMCP) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}
func (m *fakeMCP) Connect(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}
func (m *fakeMCP) Close() error {
	m.MarkDisconnected()
	return nil
}
func (m *fakeMCP) ListTools(context.Context) ([]workspace.Tool, error) {
	if !m.IsConnected() {
		return nil, fmt.Errorf("fake MCP %q is disconnected", m.Name())
	}
	return nil, nil
}
func (m *fakeMCP) MarkDisconnected() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
}
func (m *fakeMCP) MCPClientConfig() (workspace.MCPClientConfig, error) {
	if m.configErr != nil {
		return workspace.MCPClientConfig{}, m.configErr
	}
	return cloneMCPClientConfig(m.config), nil
}

type plainMCP struct{ name string }

func (m *plainMCP) Name() string                                      { return m.name }
func (*plainMCP) IsStateful() bool                                    { return false }
func (*plainMCP) IsConnected() bool                                   { return false }
func (*plainMCP) Connect(context.Context) error                       { return nil }
func (*plainMCP) Close() error                                        { return nil }
func (*plainMCP) ListTools(context.Context) ([]workspace.Tool, error) { return nil, nil }

func newWorkspaceFixture(t *testing.T) (*Workspace, *fakeBackend, *fakeProvider, *fakeGateway, *fakeCodec) {
	t.Helper()
	backend := newFakeBackend()
	provider := &fakeProvider{backend: backend}
	gatewayClient := &fakeGateway{}
	codec := &fakeCodec{}
	w, err := New(Config{
		ID:          "sandbox-test",
		Workdir:     "/work",
		GatewayHome: "/gateway",
		GatewayPort: 5600,
		Provider:    provider,
		GatewayFactory: func(Backend, int, time.Duration) (Gateway, error) {
			return gatewayClient, nil
		},
		MCPCodec: codec,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return w, backend, provider, gatewayClient, codec
}

func readyWorkspace(t *testing.T) (*Workspace, *fakeBackend, *fakeProvider, *fakeGateway, *fakeCodec) {
	t.Helper()
	w, backend, provider, gatewayClient, codec := newWorkspaceFixture(t)
	markGatewayBootstrapCurrent(w, backend)
	w.backend = backend
	w.gateway = gatewayClient
	w.gatewayGate = &operationGate{}
	w.alive = true
	return w, backend, provider, gatewayClient, codec
}

func markGatewayBootstrapCurrent(w *Workspace, backend *fakeBackend) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.files[w.gatewayPython] = []byte("python")
	backend.files[w.gatewayScript] = bytes.Clone(gatewayAppScript)
	backend.files[w.gatewayMarker] = []byte(w.gatewayVersion + "\n")
}

func toolChunkResult(chunks <-chan tool.ToolChunk, err error) (message.ToolResultState, string) {
	if err != nil {
		return message.ToolResultError, "unexpected execution error: " + err.Error()
	}
	chunk, ok := <-chunks
	if !ok {
		return message.ToolResultError, "tool execution returned no chunk"
	}
	if _, extra := <-chunks; extra {
		return message.ToolResultError, "tool execution returned multiple chunks"
	}
	text := chunk.Content.GetTextContent("")
	if text == nil {
		return chunk.State, ""
	}
	return chunk.State, *text
}

func requireErrorContains(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("error = %v, want substring %q", err, text)
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

var (
	_ Backend                     = (*fakeBackend)(nil)
	_ Provider                    = (*fakeProvider)(nil)
	_ Gateway                     = (*fakeGateway)(nil)
	_ MCPCodec                    = (*fakeCodec)(nil)
	_ workspace.MCPClient         = (*fakeMCP)(nil)
	_ workspace.MCPConfigProvider = (*fakeMCP)(nil)
	_ workspace.MCPClient         = (*plainMCP)(nil)
)
