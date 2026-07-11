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
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

func validConfig() Config {
	backend := newFakeBackend()
	return Config{
		ID:          "test",
		Workdir:     "/work",
		GatewayHome: "/gateway",
		GatewayPort: 5600,
		Provider:    &fakeProvider{backend: backend},
		GatewayFactory: func(Backend, int, time.Duration) (Gateway, error) {
			return &fakeGateway{}, nil
		},
		MCPCodec: &fakeCodec{},
	}
}

func TestNewValidationAndDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "empty id", mutate: func(config *Config) { config.ID = " " }, want: "workspace id"},
		{name: "empty workdir", mutate: func(config *Config) { config.Workdir = "" }, want: "invalid workdir"},
		{name: "relative workdir", mutate: func(config *Config) { config.Workdir = "work" }, want: "absolute"},
		{name: "root workdir", mutate: func(config *Config) { config.Workdir = "/" }, want: "root directory"},
		{name: "NUL workdir", mutate: func(config *Config) { config.Workdir = "/bad\x00path" }, want: "NUL"},
		{name: "gateway home", mutate: func(config *Config) { config.GatewayHome = "gateway" }, want: "invalid gateway home"},
		{name: "port zero", mutate: func(config *Config) { config.GatewayPort = 0 }, want: "between 1 and 65535"},
		{name: "port large", mutate: func(config *Config) { config.GatewayPort = 65536 }, want: "between 1 and 65535"},
		{name: "provider", mutate: func(config *Config) { config.Provider = nil }, want: "provider is nil"},
		{name: "gateway factory", mutate: func(config *Config) { config.GatewayFactory = nil }, want: "gateway factory is nil"},
		{name: "codec", mutate: func(config *Config) { config.MCPCodec = nil }, want: "MCP codec is nil"},
		{name: "empty bootstrap", mutate: func(config *Config) { config.BootstrapCommands = [][]string{nil} }, want: "bootstrap command 0"},
		{name: "blank bootstrap", mutate: func(config *Config) { config.BootstrapCommands = [][]string{{" "}} }, want: "bootstrap command 0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			test.mutate(&config)
			_, err := New(config)
			requireErrorContains(t, err, test.want)
		})
	}

	config := validConfig()
	config.ID = "  stable-id  "
	config.Workdir = "/work/../work/root"
	config.GatewayHome = "/gateway/./home"
	config.BootstrapCommands = [][]string{{"python", "-m", "pip"}}
	config.SkillPaths = []string{"one"}
	w, err := New(config)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	config.BootstrapCommands[0][0] = "changed"
	config.SkillPaths[0] = "changed"
	if w.id != "stable-id" || w.workdir != "/work/root" || w.gatewayHome != "/gateway/home" || w.gatewayTimeout != defaultGatewayTimeout {
		t.Fatalf("unexpected normalized workspace: %#v", w)
	}
	if w.instructions != defaultInstructions || w.bootstrapCommands[0][0] != "python" || w.skillPaths[0] != "one" {
		t.Fatal("New did not apply defaults or defensive copies")
	}
}

func TestWorkspaceIdentityInstructionsAndLists(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	if nilWorkspace.WorkspaceID() != "" || nilWorkspace.WorkspaceRoot() != "" || nilWorkspace.IsAlive() {
		t.Fatal("nil workspace identity should be empty")
	}
	if _, err := nilWorkspace.GetInstructions(context.Background()); err == nil {
		t.Fatal("nil GetInstructions should fail")
	}
	if _, err := nilWorkspace.ListMCPs(context.Background()); err == nil {
		t.Fatal("nil ListMCPs should fail")
	}
	if err := nilWorkspace.Close(context.Background()); err != nil || nilWorkspace.Reset(context.Background()) != nil || nilWorkspace.RemoveMCP(context.Background(), "x") != nil {
		t.Fatal("nil lifecycle no-ops should succeed")
	}

	w, _, _, _, _ := readyWorkspace(t)
	if w.WorkspaceID() != "sandbox-test" || w.WorkspaceRoot() != "/work" || !w.IsAlive() {
		t.Fatal("workspace identity or liveness mismatch")
	}
	instructions, err := w.GetInstructions(context.Background())
	if err != nil || !strings.Contains(instructions, "/work") || strings.Contains(instructions, "{workdir}") {
		t.Fatalf("GetInstructions = %q, %v", instructions, err)
	}
	if _, err := w.GetInstructions(canceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled GetInstructions error = %v", err)
	}
	w.mcps = []workspace.MCPClient{&fakeMCP{config: httpMCPConfig("one")}}
	listed, err := w.ListMCPs(context.Background())
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListMCPs = %#v, %v", listed, err)
	}
	listed[0] = nil
	if w.mcps[0] == nil {
		t.Fatal("ListMCPs must copy its slice")
	}
	if _, err := w.ListMCPs(canceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ListMCPs error = %v", err)
	}
}

func TestInitializeHappyPathIsIdempotent(t *testing.T) {
	t.Parallel()

	w, backend, provider, gatewayClient, _ := newWorkspaceFixture(t)
	if err := w.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if !w.IsAlive() || w.backend != backend || w.gateway != gatewayClient || len(w.mcps) != 0 {
		t.Fatalf("unexpected initialized state: %#v", w)
	}
	if len(backend.file(w.gatewayScript)) == 0 || string(backend.file(w.mcpFile)) != "[]" {
		t.Fatal("Initialize did not upload gateway script or seed MCP file")
	}
	if len(backend.callsFor("sh")) != 1 {
		t.Fatalf("gateway launch calls = %#v", backend.callsFor("sh"))
	}
	if err := w.Initialize(context.Background()); err != nil {
		t.Fatalf("second Initialize returned error: %v", err)
	}
	openCalls, closeCalls := provider.counts()
	if openCalls != 1 || closeCalls != 0 {
		t.Fatalf("provider calls after idempotent init = %d/%d", openCalls, closeCalls)
	}
	if err := w.Initialize(canceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Initialize error = %v", err)
	}
}

func TestInitializeRestoresPersistedAndFallsBackToDefaults(t *testing.T) {
	t.Parallel()

	t.Run("persisted", func(t *testing.T) {
		w, backend, _, gatewayClient, codec := newWorkspaceFixture(t)
		persisted := []workspace.MCPClientConfig{httpMCPConfig("persisted")}
		data, err := codec.Marshal(persisted)
		if err != nil {
			t.Fatal(err)
		}
		backend.files[w.mcpFile] = data
		markGatewayBootstrapCurrent(w, backend)
		gatewayClient.configs = persisted
		if err := w.Initialize(context.Background()); err != nil {
			t.Fatalf("Initialize returned error: %v", err)
		}
		if len(w.mcps) != 1 || w.mcps[0].Name() != "persisted" {
			t.Fatalf("restored MCPs = %#v", w.mcps)
		}
		if len(backend.writeCalls) != 1 || backend.writeCalls[0] != w.mcpFile {
			t.Fatalf("existing gateway script should not be uploaded: %#v", backend.writeCalls)
		}
	})

	t.Run("invalid persisted falls back and rewrites", func(t *testing.T) {
		w, backend, _, gatewayClient, codec := newWorkspaceFixture(t)
		defaults := []workspace.MCPClientConfig{httpMCPConfig("default")}
		w.defaultMCPs = []workspace.MCPClient{&fakeMCP{config: defaults[0]}}
		gatewayClient.configs = defaults
		backend.files[w.mcpFile] = []byte("invalid")
		if err := w.Initialize(context.Background()); err != nil {
			t.Fatalf("Initialize returned error: %v", err)
		}
		if len(w.mcps) != 1 || w.mcps[0].Name() != "default" {
			t.Fatalf("fallback MCPs = %#v", w.mcps)
		}
		rewritten, err := codec.Unmarshal(backend.file(w.mcpFile))
		if err != nil || len(rewritten) != 1 || rewritten[0].Name != "default" {
			t.Fatalf("rewritten MCP file = %#v, %v", rewritten, err)
		}
	})

	t.Run("read failure propagates without overwrite", func(t *testing.T) {
		w, backend, provider, gatewayClient, _ := newWorkspaceFixture(t)
		w.defaultMCPs = []workspace.MCPClient{&fakeMCP{config: httpMCPConfig("default")}}
		original := []byte("preserve-on-read-error")
		backend.files[w.mcpFile] = original
		readFailure := errors.New("read failed")
		backend.readHook = func(_ context.Context, current string) ([]byte, error, bool) {
			if current == w.mcpFile {
				return nil, readFailure, true
			}
			return nil, nil, false
		}
		err := w.Initialize(context.Background())
		requireErrorContains(t, err, "read MCP file")
		if !errors.Is(err, readFailure) {
			t.Fatalf("Initialize error does not wrap read failure: %v", err)
		}
		_, closes := provider.counts()
		if closes != 1 || gatewayClient.bootstrapCalls != 0 || len(backend.writeCalls) != 0 || string(backend.file(w.mcpFile)) != string(original) {
			t.Fatalf("read failure mutated state: closes=%d bootstrap=%d writes=%#v file=%q", closes, gatewayClient.bootstrapCalls, backend.writeCalls, backend.file(w.mcpFile))
		}
		if w.alive || w.backend != nil || w.gateway != nil || w.mcps != nil {
			t.Fatal("read failure did not roll back workspace state")
		}
	})
}

func TestInitializeSeedsSkillsAndRollsBackSeedFailure(t *testing.T) {
	t.Parallel()

	source := writeLocalSkill(t, "Seed Skill", "seeded during initialize", nil)
	w, backend, _, _, _ := newWorkspaceFixture(t)
	w.skillPaths = []string{source}
	if err := w.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if len(backend.file("/work/skills/seed-skill/SKILL.md")) == 0 {
		t.Fatal("Initialize did not seed the configured skill")
	}

	w2, _, provider2, _, _ := newWorkspaceFixture(t)
	w2.skillPaths = []string{filepath.Join(t.TempDir(), "missing")}
	err := w2.Initialize(context.Background())
	requireErrorContains(t, err, "seed skill")
	_, closes := provider2.counts()
	if closes != 1 || w2.alive || w2.backend != nil {
		t.Fatalf("seed failure did not roll back: closes=%d alive=%t backend=%v", closes, w2.alive, w2.backend)
	}
}

func TestInitializeValidationAndRollback(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	if err := nilWorkspace.Initialize(context.Background()); err == nil {
		t.Fatal("nil Initialize should fail")
	}
	w, _, provider, _, _ := newWorkspaceFixture(t)
	if err := w.Initialize(canceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Initialize error = %v", err)
	}
	if open, _ := provider.counts(); open != 0 {
		t.Fatal("canceled Initialize should not open provider")
	}

	t.Run("open error", func(t *testing.T) {
		w, _, provider, _, _ := newWorkspaceFixture(t)
		provider.openErr = errors.New("open failed")
		requireErrorContains(t, w.Initialize(context.Background()), "open provider")
		_, closes := provider.counts()
		if closes != 0 {
			t.Fatal("failed Open should not Close provider")
		}
	})

	t.Run("nil backend", func(t *testing.T) {
		w, _, provider, _, _ := newWorkspaceFixture(t)
		provider.backend = nil
		requireErrorContains(t, w.Initialize(context.Background()), "nil backend")
		_, closes := provider.counts()
		if closes != 1 {
			t.Fatalf("nil backend close calls = %d", closes)
		}
	})

	t.Run("layout rollback joins close", func(t *testing.T) {
		w, backend, provider, _, _ := newWorkspaceFixture(t)
		provider.closeErr = errors.New("close failed")
		backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
			if len(argv) > 0 && argv[0] == "mkdir" {
				return ExecResult{}, errors.New("mkdir failed"), true
			}
			return ExecResult{}, nil, false
		}
		err := w.Initialize(context.Background())
		requireErrorContains(t, err, "mkdir failed")
		requireErrorContains(t, err, "rollback provider")
		if w.backend != nil || w.gateway != nil || w.alive {
			t.Fatal("failed Initialize must clear state")
		}
	})

	t.Run("layout nonzero", func(t *testing.T) {
		w, backend, _, _, _ := newWorkspaceFixture(t)
		backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
			if len(argv) > 0 && argv[0] == "mkdir" {
				return ExecResult{ExitCode: 4, Stderr: []byte("denied")}, nil, true
			}
			return ExecResult{}, nil, false
		}
		requireErrorContains(t, w.Initialize(context.Background()), "exit code 4")
	})

	t.Run("default MCP config", func(t *testing.T) {
		w, _, _, _, _ := newWorkspaceFixture(t)
		w.defaultMCPs = []workspace.MCPClient{&fakeMCP{config: httpMCPConfig("bad"), configErr: errors.New("config failed")}}
		requireErrorContains(t, w.Initialize(context.Background()), "config failed")
	})
}

func TestSetupGatewayFailureMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*Workspace, *fakeBackend, *fakeProvider, *fakeGateway, *fakeCodec)
		want      string
	}{
		{
			name: "MCP file stat error",
			configure: func(w *Workspace, backend *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
					if len(argv) == 3 && argv[0] == "test" && argv[2] == w.mcpFile {
						return ExecResult{}, errors.New("stat failed"), true
					}
					return ExecResult{}, nil, false
				}
			},
			want: "test file",
		},
		{
			name: "MCP encode",
			configure: func(_ *Workspace, _ *fakeBackend, _ *fakeProvider, _ *fakeGateway, codec *fakeCodec) {
				codec.marshalErr = errors.New("encode failed")
			},
			want: "encode MCP file",
		},
		{
			name: "MCP write",
			configure: func(w *Workspace, backend *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				backend.writeHook = func(_ context.Context, filename string, _ []byte) (error, bool) {
					if filename == w.mcpFile {
						return errors.New("write failed"), true
					}
					return nil, false
				}
			},
			want: "write MCP file",
		},
		{
			name: "gateway script stat nonzero",
			configure: func(w *Workspace, backend *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				backend.files[w.gatewayPython] = []byte("python")
				backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
					if len(argv) == 3 && argv[0] == "test" && argv[2] == w.gatewayScript {
						return ExecResult{ExitCode: 3, Stderr: []byte("stat denied")}, nil, true
					}
					return ExecResult{}, nil, false
				}
			},
			want: "exit code 3",
		},
		{
			name: "bootstrap exec",
			configure: func(w *Workspace, backend *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				w.bootstrapCommands = [][]string{{"bootstrap"}}
				backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
					if len(argv) > 0 && argv[0] == "bootstrap" {
						return ExecResult{}, errors.New("bootstrap failed"), true
					}
					return ExecResult{}, nil, false
				}
			},
			want: "bootstrap command 0",
		},
		{
			name: "bootstrap nonzero",
			configure: func(w *Workspace, backend *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				w.bootstrapCommands = [][]string{{"bootstrap"}}
				backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
					if len(argv) > 0 && argv[0] == "bootstrap" {
						return ExecResult{ExitCode: 5, Stderr: []byte("bad bootstrap")}, nil, true
					}
					return ExecResult{}, nil, false
				}
			},
			want: "exit code 5",
		},
		{
			name: "upload script",
			configure: func(w *Workspace, backend *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				backend.writeHook = func(_ context.Context, filename string, _ []byte) (error, bool) {
					if filename == w.gatewayScript {
						return errors.New("upload failed"), true
					}
					return nil, false
				}
			},
			want: "upload gateway script",
		},
		{
			name: "launch exec",
			configure: func(_ *Workspace, backend *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
					if len(argv) > 2 && argv[0] == "sh" && argv[2] == launchGatewayScript {
						return ExecResult{}, errors.New("launch failed"), true
					}
					return ExecResult{}, nil, false
				}
			},
			want: "launch gateway",
		},
		{
			name: "launch nonzero",
			configure: func(_ *Workspace, backend *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
					if len(argv) > 2 && argv[0] == "sh" && argv[2] == launchGatewayScript {
						return ExecResult{ExitCode: 6, Stderr: []byte("launch bad")}, nil, true
					}
					return ExecResult{}, nil, false
				}
			},
			want: "exit code 6",
		},
		{
			name: "factory",
			configure: func(w *Workspace, _ *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				w.gatewayFactory = func(Backend, int, time.Duration) (Gateway, error) {
					return nil, errors.New("factory failed")
				}
			},
			want: "create gateway client",
		},
		{
			name: "nil factory client",
			configure: func(w *Workspace, _ *fakeBackend, _ *fakeProvider, _ *fakeGateway, _ *fakeCodec) {
				w.gatewayFactory = func(Backend, int, time.Duration) (Gateway, error) {
					return nil, nil
				}
			},
			want: "gateway factory returned nil client",
		},
		{
			name: "list gateway",
			configure: func(_ *Workspace, _ *fakeBackend, _ *fakeProvider, gatewayClient *fakeGateway, _ *fakeCodec) {
				gatewayClient.listErr = errors.New("list failed")
			},
			want: "list gateway MCPs",
		},
		{
			name: "count mismatch",
			configure: func(_ *Workspace, _ *fakeBackend, _ *fakeProvider, gatewayClient *fakeGateway, _ *fakeCodec) {
				gatewayClient.configs = []workspace.MCPClientConfig{httpMCPConfig("unexpected")}
			},
			want: "expected 0",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w, backend, provider, gatewayClient, codec := newWorkspaceFixture(t)
			test.configure(w, backend, provider, gatewayClient, codec)
			err := w.Initialize(context.Background())
			requireErrorContains(t, err, test.want)
			_, closes := provider.counts()
			if closes != 1 || w.backend != nil || w.alive {
				t.Fatalf("Initialize rollback state: closes=%d backend=%v alive=%t", closes, w.backend, w.alive)
			}
		})
	}
}

func TestSetupGatewayRetryCancellationAndTimeout(t *testing.T) {
	t.Parallel()

	t.Run("retry succeeds", func(t *testing.T) {
		w, _, _, gatewayClient, _ := readyWorkspace(t)
		gatewayClient.bootstrapErrors = []error{errors.New("not ready"), nil}
		w.gatewayTimeout = time.Second
		client, gate, err := w.setupGateway(context.Background())
		if err != nil || client != gatewayClient || gate == nil || gatewayClient.bootstrapCalls != 2 {
			t.Fatalf("setupGateway retry = %#v, %v calls=%d", client, err, gatewayClient.bootstrapCalls)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		w, _, _, gatewayClient, _ := readyWorkspace(t)
		gatewayClient.bootstrapErrors = []error{errors.New("not ready"), errors.New("not ready")}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()
		if _, _, err := w.setupGateway(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("setupGateway canceled error = %v", err)
		}
	})

	t.Run("deadline with log tail", func(t *testing.T) {
		w, backend, _, gatewayClient, _ := readyWorkspace(t)
		backend.files[w.gatewayLog] = []byte("old\nlast crash")
		gatewayClient.bootstrapErrors = []error{errors.New("not ready")}
		w.gatewayTimeout = time.Millisecond
		_, _, err := w.setupGateway(context.Background())
		requireErrorContains(t, err, "did not become healthy")
		requireErrorContains(t, err, "last crash")
		delete(backend.files, w.gatewayLog)
		if got := w.gatewayLogTail(context.Background(), 10); got != "<unavailable>" {
			t.Fatalf("unavailable log tail = %q", got)
		}
	})
}

func TestSetupGatewayBootstrapFreshness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mutate        func(*Workspace, *fakeBackend)
		wantBootstrap int
		wantWrites    int
	}{
		{
			name: "current skips bootstrap",
		},
		{
			name: "marker mismatch",
			mutate: func(w *Workspace, backend *fakeBackend) {
				backend.files[w.gatewayMarker] = []byte("stale\n")
			},
			wantBootstrap: 1,
			wantWrites:    2,
		},
		{
			name: "script mismatch",
			mutate: func(w *Workspace, backend *fakeBackend) {
				backend.files[w.gatewayScript] = []byte("stale")
			},
			wantBootstrap: 1,
			wantWrites:    2,
		},
		{
			name: "python missing",
			mutate: func(w *Workspace, backend *fakeBackend) {
				delete(backend.files, w.gatewayPython)
			},
			wantBootstrap: 1,
			wantWrites:    2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w, backend, _, gatewayClient, _ := readyWorkspace(t)
			w.bootstrapCommands = [][]string{{"bootstrap", "--install"}}
			w.gatewayVersion = bootstrapFingerprint(w.bootstrapCommands)
			markGatewayBootstrapCurrent(w, backend)
			if test.mutate != nil {
				test.mutate(w, backend)
			}
			backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
				if len(argv) == 0 || argv[0] != "bootstrap" {
					return ExecResult{}, nil, false
				}
				backend.mu.Lock()
				backend.files[w.gatewayPython] = []byte("python")
				backend.mu.Unlock()
				return ExecResult{ExitCode: 0}, nil, true
			}

			client, gate, err := w.setupGateway(context.Background())
			if err != nil || client != gatewayClient || gate == nil {
				t.Fatalf("setupGateway = %#v, %v", client, err)
			}
			if got := len(backend.callsFor("bootstrap")); got != test.wantBootstrap {
				t.Fatalf("bootstrap calls = %d, want %d", got, test.wantBootstrap)
			}
			if len(backend.writeCalls) != test.wantWrites {
				t.Fatalf("write calls = %#v, want %d", backend.writeCalls, test.wantWrites)
			}
			if test.wantWrites > 0 && (backend.writeCalls[0] != w.gatewayScript || backend.writeCalls[1] != w.gatewayMarker) {
				t.Fatalf("bootstrap write order = %#v", backend.writeCalls)
			}
			if string(backend.file(w.gatewayScript)) != string(gatewayAppScript) || strings.TrimSpace(string(backend.file(w.gatewayMarker))) != w.gatewayVersion {
				t.Fatal("setupGateway did not persist the current script and marker")
			}
		})
	}
}

func TestSetupGatewayWritesMarkerLastAndRetriesAfterMarkerFailure(t *testing.T) {
	t.Parallel()

	w, backend, _, gatewayClient, _ := readyWorkspace(t)
	w.bootstrapCommands = [][]string{{"bootstrap", "--install"}}
	w.gatewayVersion = bootstrapFingerprint(w.bootstrapCommands)
	markGatewayBootstrapCurrent(w, backend)
	delete(backend.files, w.gatewayScript)
	delete(backend.files, w.gatewayMarker)
	markerWrites := 0
	markerFailure := errors.New("marker failed")
	backend.writeHook = func(_ context.Context, filename string, _ []byte) (error, bool) {
		if filename != w.gatewayMarker {
			return nil, false
		}
		markerWrites++
		if markerWrites == 1 {
			return markerFailure, true
		}
		return nil, false
	}

	_, _, err := w.setupGateway(context.Background())
	requireErrorContains(t, err, "write gateway bootstrap marker")
	if !errors.Is(err, markerFailure) {
		t.Fatalf("setupGateway error does not wrap marker failure: %v", err)
	}
	if backend.file(w.gatewayMarker) != nil || len(backend.writeCalls) != 2 || backend.writeCalls[0] != w.gatewayScript || backend.writeCalls[1] != w.gatewayMarker {
		t.Fatalf("first bootstrap writes = %#v marker=%q", backend.writeCalls, backend.file(w.gatewayMarker))
	}

	client, gate, err := w.setupGateway(context.Background())
	if err != nil || client != gatewayClient || gate == nil {
		t.Fatalf("retry setupGateway = %#v, %v", client, err)
	}
	if got := len(backend.callsFor("bootstrap")); got != 2 {
		t.Fatalf("bootstrap calls after retry = %d", got)
	}
	wantWrites := []string{w.gatewayScript, w.gatewayMarker, w.gatewayScript, w.gatewayMarker}
	if len(backend.writeCalls) != len(wantWrites) {
		t.Fatalf("bootstrap write calls = %#v", backend.writeCalls)
	}
	for index := range wantWrites {
		if backend.writeCalls[index] != wantWrites[index] {
			t.Fatalf("bootstrap write calls = %#v", backend.writeCalls)
		}
	}
	if strings.TrimSpace(string(backend.file(w.gatewayMarker))) != w.gatewayVersion {
		t.Fatal("successful retry did not persist the bootstrap marker")
	}
}

func TestBootstrapFingerprint(t *testing.T) {
	t.Parallel()

	commands := [][]string{{"python", "-m", "pip"}, {"uv", "sync"}}
	if first, second := bootstrapFingerprint(commands), bootstrapFingerprint(cloneArgvList(commands)); first != second {
		t.Fatalf("bootstrap fingerprint is unstable: %q != %q", first, second)
	}
	base := bootstrapFingerprint([][]string{{"ab", "c"}})
	for _, changed := range [][][]string{
		{{"ab", "d"}},
		{{"a", "bc"}},
		{{"ab", "c"}, {"extra"}},
	} {
		if got := bootstrapFingerprint(changed); got == base {
			t.Fatalf("bootstrap fingerprint did not change for %#v", changed)
		}
	}
}

func TestDeleteTreeScriptRemovesDanglingSymlink(t *testing.T) {
	t.Parallel()

	link := filepath.Join(t.TempDir(), "dangling")
	if err := os.Symlink("missing-target", link); err != nil {
		t.Fatalf("create dangling symlink: %v", err)
	}
	command := exec.CommandContext(context.Background(), "sh", "-c", deleteTreeScript, "--", link)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("deleteTreeScript returned error: %v: %s", err, output)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dangling symlink still exists: %v", err)
	}
}

func TestCloseClearsStateAndJoinsErrors(t *testing.T) {
	t.Parallel()

	w, _, provider, gatewayClient, _ := readyWorkspace(t)
	gatewayClient.closeErr = errors.New("gateway close failed")
	provider.closeErr = errors.New("provider close failed")
	err := w.Close(context.Background())
	requireErrorContains(t, err, "close gateway")
	requireErrorContains(t, err, "close provider")
	if w.gateway != nil || w.backend != nil || w.mcps != nil || w.alive {
		t.Fatal("Close must clear local state even when dependencies fail")
	}
	provider.closeErr = nil
	if err := w.Close(context.Background()); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
	_, providerCloses := provider.counts()
	if providerCloses != 2 || gatewayClient.closeCalls != 1 {
		t.Fatalf("Close retry calls: provider=%d gateway=%d", providerCloses, gatewayClient.closeCalls)
	}
}

func TestCloseWaitsForInFlightRemoteTool(t *testing.T) {
	t.Parallel()

	w, backend, provider, _, _ := readyWorkspace(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	backend.execHook = func(ctx context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
		if len(argv) == 0 || argv[0] != "/bin/bash" {
			return ExecResult{}, nil, false
		}
		close(started)
		select {
		case <-release:
			return ExecResult{ExitCode: 0, Stdout: []byte("done")}, nil, true
		case <-ctx.Done():
			return ExecResult{}, ctx.Err(), true
		}
	}

	type toolOutcome struct {
		state message.ToolResultState
		text  string
	}
	toolDone := make(chan toolOutcome, 1)
	remote := newRemoteTools(w)[0]
	go func() {
		stateValue, text := toolChunkResult(remote.Execute(context.Background(), map[string]any{"command": "work"}, nil))
		toolDone <- toolOutcome{state: stateValue, text: text}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("remote tool did not reach the backend")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- w.Close(context.Background()) }()
	select {
	case err := <-closeDone:
		releaseOnce.Do(func() { close(release) })
		t.Fatalf("Close returned before the in-flight tool completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case outcome := <-toolDone:
		if outcome.state != message.ToolResultSuccess || outcome.text != "done" {
			t.Fatalf("remote tool outcome = %s %q", outcome.state, outcome.text)
		}
	case <-time.After(time.Second):
		t.Fatal("remote tool did not finish")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close remained blocked after the tool completed")
	}
	_, providerCloses := provider.counts()
	if providerCloses != 1 || w.IsAlive() {
		t.Fatalf("Close final state: provider closes=%d alive=%t", providerCloses, w.IsAlive())
	}
}

func TestResetSuccess(t *testing.T) {
	t.Parallel()

	w, backend, _, gatewayClient, codec := readyWorkspace(t)
	w.mcps = []workspace.MCPClient{
		&fakeMCP{config: httpMCPConfig("one")}, nil, &fakeMCP{config: httpMCPConfig("two")},
	}
	gatewayClient.configs = []workspace.MCPClientConfig{
		httpMCPConfig("one"), httpMCPConfig("two"), httpMCPConfig("orphan"),
	}
	backend.files[w.dataDir+"/data.bin"] = []byte("data")
	backend.files[w.skillsDir+"/skill/SKILL.md"] = []byte("skill")
	backend.files[w.sessionsDir+"/session/context.jsonl"] = []byte("session")
	if err := w.Reset(context.Background()); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	if w.mcps != nil || string(backend.file(w.mcpFile)) != "[]" || backend.file(w.dataDir+"/data.bin") != nil {
		t.Fatal("Reset did not clear workspace state")
	}
	if len(gatewayClient.removeCalls) != 3 {
		t.Fatalf("Reset remove calls = %#v", gatewayClient.removeCalls)
	}
	configs, err := gatewayClient.ListMCPs(context.Background())
	if err != nil || len(configs) != 0 {
		t.Fatalf("gateway MCPs after Reset = %#v, %v", configs, err)
	}
	if _, err := codec.Unmarshal(backend.file(w.mcpFile)); err != nil {
		t.Fatalf("cleared MCP file is invalid: %v", err)
	}

	var nilWorkspace *Workspace
	if err := nilWorkspace.Reset(context.Background()); err != nil {
		t.Fatalf("nil Reset returned error: %v", err)
	}
	w2, _, _, _, _ := newWorkspaceFixture(t)
	requireErrorContains(t, w2.Reset(context.Background()), "not initialized")
	if err := w2.Reset(canceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Reset error = %v", err)
	}
}

func TestResetEncodingFailureDoesNotMutateMCPSnapshots(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		configure func(*fakeCodec)
		want      string
	}{
		{
			name: "empty snapshot encoding",
			configure: func(codec *fakeCodec) {
				codec.marshalHook = func(configs []workspace.MCPClientConfig) ([]byte, error) {
					if len(configs) == 0 {
						return nil, errors.New("encode empty failed")
					}
					return json.Marshal(configs)
				}
			},
			want: "encode empty MCP file",
		},
		{
			name: "current snapshot encoding",
			configure: func(codec *fakeCodec) {
				calls := 0
				codec.marshalHook = func(configs []workspace.MCPClientConfig) ([]byte, error) {
					calls++
					if calls == 2 {
						return nil, errors.New("encode current failed")
					}
					return json.Marshal(configs)
				}
			},
			want: "encode current MCP file",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			w, backend, _, gatewayClient, codec := readyWorkspace(t)
			configs := []workspace.MCPClientConfig{httpMCPConfig("one"), httpMCPConfig("two")}
			w.mcps = []workspace.MCPClient{&fakeMCP{config: configs[0]}, &fakeMCP{config: configs[1]}}
			gatewayClient.configs = append([]workspace.MCPClientConfig(nil), configs...)
			originalFile, err := codec.Marshal(configs)
			if err != nil {
				t.Fatal(err)
			}
			backend.files[w.mcpFile] = originalFile
			test.configure(codec)
			err = w.Reset(context.Background())
			requireErrorContains(t, err, test.want)
			if len(gatewayClient.removeCalls) != 0 || len(gatewayClient.addCalls) != 0 {
				t.Fatalf("encoding failure touched gateway: remove=%#v add=%#v", gatewayClient.removeCalls, gatewayClient.addCalls)
			}
			assertMCPSnapshots(t, w, backend, gatewayClient, []string{"one", "two"}, []string{"one", "two"}, originalFile)
		})
	}
}

func TestResetGatewayListFailureDoesNotMutateMCPSnapshots(t *testing.T) {
	t.Parallel()

	w, backend, _, gatewayClient, codec := readyWorkspace(t)
	configs := []workspace.MCPClientConfig{httpMCPConfig("one")}
	liveConfigs := []workspace.MCPClientConfig{configs[0], httpMCPConfig("orphan")}
	w.mcps = []workspace.MCPClient{&fakeMCP{config: configs[0]}}
	gatewayClient.configs = append([]workspace.MCPClientConfig(nil), liveConfigs...)
	originalFile, err := codec.Marshal(configs)
	if err != nil {
		t.Fatal(err)
	}
	backend.files[w.mcpFile] = originalFile
	listFailure := errors.New("list failed")
	gatewayClient.listErr = listFailure

	err = w.Reset(context.Background())
	requireErrorContains(t, err, "list gateway MCPs before reset")
	if !errors.Is(err, listFailure) {
		t.Fatalf("Reset error does not wrap list failure: %v", err)
	}
	if len(gatewayClient.removeCalls) != 0 || len(gatewayClient.addCalls) != 0 || len(backend.writeCalls) != 0 {
		t.Fatalf("list failure mutated MCP state: remove=%#v add=%#v writes=%#v", gatewayClient.removeCalls, gatewayClient.addCalls, backend.writeCalls)
	}
	gatewayClient.listErr = nil
	assertMCPSnapshots(t, w, backend, gatewayClient, []string{"one"}, []string{"one", "orphan"}, originalFile)
}

func TestResetRemoveFailureRestoresMCPSnapshots(t *testing.T) {
	t.Parallel()

	w, backend, _, gatewayClient, codec := readyWorkspace(t)
	configs := []workspace.MCPClientConfig{httpMCPConfig("one"), httpMCPConfig("two")}
	w.mcps = []workspace.MCPClient{&fakeMCP{config: configs[0]}, &fakeMCP{config: configs[1]}}
	gatewayClient.configs = append([]workspace.MCPClientConfig(nil), configs...)
	originalFile, err := codec.Marshal(configs)
	if err != nil {
		t.Fatal(err)
	}
	backend.files[w.mcpFile] = originalFile
	removeFailure := errors.New("remove failed")
	gatewayClient.removeErrors = []error{nil, removeFailure}
	err = w.Reset(context.Background())
	requireErrorContains(t, err, "remove MCP \"two\"")
	if !errors.Is(err, removeFailure) {
		t.Fatalf("Reset error does not wrap remove failure: %v", err)
	}
	if len(gatewayClient.removeCalls) != 2 || len(gatewayClient.addCalls) != 1 || gatewayClient.addCalls[0].Name != "one" || len(backend.writeCalls) != 0 {
		t.Fatalf("unexpected rollback calls: remove=%#v add=%#v writes=%#v", gatewayClient.removeCalls, gatewayClient.addCalls, backend.writeCalls)
	}
	assertMCPSnapshots(t, w, backend, gatewayClient, []string{"one", "two"}, []string{"one", "two"}, originalFile)
}

func TestResetWriteFailureRestoresGatewayAndFileSnapshots(t *testing.T) {
	t.Parallel()

	w, backend, _, gatewayClient, codec := readyWorkspace(t)
	configs := []workspace.MCPClientConfig{httpMCPConfig("one"), httpMCPConfig("two")}
	w.mcps = []workspace.MCPClient{&fakeMCP{config: configs[0]}, &fakeMCP{config: configs[1]}}
	gatewayClient.configs = append([]workspace.MCPClientConfig(nil), configs...)
	originalFile, err := codec.Marshal(configs)
	if err != nil {
		t.Fatal(err)
	}
	backend.files[w.mcpFile] = originalFile
	writes := 0
	writeFailure := errors.New("clear failed")
	backend.writeHook = func(_ context.Context, filename string, _ []byte) (error, bool) {
		if filename != w.mcpFile {
			return nil, false
		}
		writes++
		if writes == 1 {
			return writeFailure, true
		}
		return nil, false
	}
	err = w.Reset(context.Background())
	requireErrorContains(t, err, "clear MCP file")
	if !errors.Is(err, writeFailure) {
		t.Fatalf("Reset error does not wrap write failure: %v", err)
	}
	if len(gatewayClient.removeCalls) != 2 || len(gatewayClient.addCalls) != 2 || writes != 2 {
		t.Fatalf("unexpected rollback calls: remove=%#v add=%#v writes=%d", gatewayClient.removeCalls, gatewayClient.addCalls, writes)
	}
	assertMCPSnapshots(t, w, backend, gatewayClient, []string{"one", "two"}, []string{"one", "two"}, originalFile)
}

func TestAddMCPValidationSuccessAndRollback(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	requireErrorContains(t, nilWorkspace.AddMCP(context.Background(), nil), "nil workspace")
	w, _, _, _, _ := newWorkspaceFixture(t)
	requireErrorContains(t, w.AddMCP(context.Background(), &fakeMCP{config: httpMCPConfig("one")}), "not initialized")
	w, backend, _, gatewayClient, codec := readyWorkspace(t)
	if err := w.AddMCP(canceledContext(), &fakeMCP{config: httpMCPConfig("one")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled AddMCP error = %v", err)
	}
	for _, client := range []workspace.MCPClient{nil, &plainMCP{name: ""}, &plainMCP{name: "plain"}} {
		if err := w.AddMCP(context.Background(), client); err == nil {
			t.Fatalf("AddMCP(%#v) should fail", client)
		}
	}
	badConfig := &fakeMCP{config: httpMCPConfig("bad"), configErr: errors.New("config failed")}
	requireErrorContains(t, w.AddMCP(context.Background(), badConfig), "config failed")

	one := &fakeMCP{config: httpMCPConfig("one")}
	if err := w.AddMCP(context.Background(), one); err != nil {
		t.Fatalf("AddMCP returned error: %v", err)
	}
	if len(w.mcps) != 1 || w.mcps[0].Name() != "one" || len(gatewayClient.addCalls) != 1 {
		t.Fatalf("AddMCP state = %#v calls=%#v", w.mcps, gatewayClient.addCalls)
	}
	requireErrorContains(t, w.AddMCP(context.Background(), one), "duplicate MCP")

	t.Run("gateway failure", func(t *testing.T) {
		w, _, _, gatewayClient, _ := readyWorkspace(t)
		gatewayClient.addErr = errors.New("add failed")
		err := w.AddMCP(context.Background(), &fakeMCP{config: httpMCPConfig("one")})
		requireErrorContains(t, err, "add gateway MCP")
		if len(w.mcps) != 0 {
			t.Fatal("failed gateway add must not mutate local MCPs")
		}
	})

	t.Run("persistence rollback failure reconciles", func(t *testing.T) {
		w, backend, _, gatewayClient, codec := readyWorkspace(t)
		marshalCalls := 0
		codec.marshalHook = func(configs []workspace.MCPClientConfig) ([]byte, error) {
			marshalCalls++
			if marshalCalls == 1 {
				return nil, errors.New("encode failed")
			}
			return json.Marshal(configs)
		}
		gatewayClient.removeErr = errors.New("rollback failed")
		err := w.AddMCP(context.Background(), &fakeMCP{config: httpMCPConfig("one")})
		requireErrorContains(t, err, "encode MCP file")
		requireErrorContains(t, err, "rollback failed")
		if len(w.mcps) != 1 || w.mcps[0].Name() != "one" || len(gatewayClient.removeCalls) != 1 || gatewayClient.listCalls != 1 {
			t.Fatalf("reconciled AddMCP state = %#v removes=%#v lists=%d", w.mcps, gatewayClient.removeCalls, gatewayClient.listCalls)
		}
		if len(gatewayClient.createdClients) != 2 || gatewayClient.createdClients[0].IsConnected() || !gatewayClient.createdClients[1].IsConnected() {
			t.Fatalf("reconciled AddMCP proxies = %#v", gatewayClient.createdClients)
		}
		persisted, decodeErr := codec.Unmarshal(backend.file(w.mcpFile))
		if decodeErr != nil || len(persisted) != 1 || persisted[0].Name != "one" {
			t.Fatalf("reconciled MCP file = %#v, %v", persisted, decodeErr)
		}
	})

	t.Run("write rollback", func(t *testing.T) {
		w, backend, _, gatewayClient, _ := readyWorkspace(t)
		backend.writeHook = func(_ context.Context, filename string, _ []byte) (error, bool) {
			if filename == w.mcpFile {
				return errors.New("write failed"), true
			}
			return nil, false
		}
		err := w.AddMCP(context.Background(), &fakeMCP{config: httpMCPConfig("one")})
		requireErrorContains(t, err, "write MCP file")
		if len(w.mcps) != 0 || len(gatewayClient.removeCalls) != 1 {
			t.Fatal("failed write must roll back MCP")
		}
	})

	_ = backend
	_ = codec
}

func TestRemoveMCPValidationSuccessAndPersistenceFailure(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	if err := nilWorkspace.RemoveMCP(context.Background(), "one"); err != nil {
		t.Fatalf("nil RemoveMCP returned error: %v", err)
	}
	w, _, _, _, _ := newWorkspaceFixture(t)
	requireErrorContains(t, w.RemoveMCP(context.Background(), "one"), "not initialized")
	w, _, _, gatewayClient, _ := readyWorkspace(t)
	if err := w.RemoveMCP(canceledContext(), "one"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled RemoveMCP error = %v", err)
	}
	requireErrorContains(t, w.RemoveMCP(context.Background(), " "), "name is empty")
	if err := w.RemoveMCP(context.Background(), "missing"); err != nil {
		t.Fatalf("missing RemoveMCP returned error: %v", err)
	}
	w.mcps = []workspace.MCPClient{&fakeMCP{config: httpMCPConfig("one")}, &fakeMCP{config: httpMCPConfig("two")}}
	gatewayClient.configs = []workspace.MCPClientConfig{httpMCPConfig("one"), httpMCPConfig("two")}
	if err := w.RemoveMCP(context.Background(), "one"); err != nil {
		t.Fatalf("RemoveMCP returned error: %v", err)
	}
	if len(w.mcps) != 1 || w.mcps[0].Name() != "two" {
		t.Fatalf("RemoveMCP state = %#v", w.mcps)
	}

	t.Run("gateway error", func(t *testing.T) {
		w, _, _, gatewayClient, _ := readyWorkspace(t)
		w.mcps = []workspace.MCPClient{&fakeMCP{config: httpMCPConfig("one")}}
		gatewayClient.removeErr = errors.New("remove failed")
		requireErrorContains(t, w.RemoveMCP(context.Background(), "one"), "remove gateway MCP")
		if len(w.mcps) != 1 {
			t.Fatal("gateway removal failure must preserve local MCP")
		}
	})

	t.Run("persistence error restores order", func(t *testing.T) {
		w, _, _, gatewayClient, codec := readyWorkspace(t)
		w.mcps = []workspace.MCPClient{
			&fakeMCP{config: httpMCPConfig("one")},
			&fakeMCP{config: httpMCPConfig("two")},
			&fakeMCP{config: httpMCPConfig("three")},
		}
		gatewayClient.configs = []workspace.MCPClientConfig{httpMCPConfig("one"), httpMCPConfig("two"), httpMCPConfig("three")}
		codec.marshalErr = errors.New("encode failed")
		requireErrorContains(t, w.RemoveMCP(context.Background(), "two"), "encode MCP file")
		if len(w.mcps) != 3 || w.mcps[0].Name() != "one" || w.mcps[1].Name() != "two" || w.mcps[2].Name() != "three" {
			t.Fatalf("MCP order was not restored: %#v", w.mcps)
		}
	})

	t.Run("rollback failure reconciles gateway state", func(t *testing.T) {
		w, backend, _, gatewayClient, codec := readyWorkspace(t)
		one := &fakeMCP{config: httpMCPConfig("one"), connected: true}
		two := &fakeMCP{config: httpMCPConfig("two"), connected: true}
		w.mcps = []workspace.MCPClient{one, two}
		gatewayClient.configs = []workspace.MCPClientConfig{httpMCPConfig("one"), httpMCPConfig("two")}
		marshalCalls := 0
		codec.marshalHook = func(configs []workspace.MCPClientConfig) ([]byte, error) {
			marshalCalls++
			if marshalCalls == 1 {
				return nil, errors.New("encode failed")
			}
			return json.Marshal(configs)
		}
		gatewayClient.addErr = errors.New("rollback failed")

		err := w.RemoveMCP(context.Background(), "one")
		requireErrorContains(t, err, "encode MCP file")
		requireErrorContains(t, err, "rollback failed")
		if len(w.mcps) != 1 || w.mcps[0].Name() != "two" || gatewayClient.listCalls != 1 {
			t.Fatalf("reconciled RemoveMCP state = %#v lists=%d", w.mcps, gatewayClient.listCalls)
		}
		if one.IsConnected() || two.IsConnected() || len(gatewayClient.createdClients) != 1 || !gatewayClient.createdClients[0].IsConnected() {
			t.Fatalf("reconciled RemoveMCP proxies: old=%t/%t new=%#v", one.IsConnected(), two.IsConnected(), gatewayClient.createdClients)
		}
		persisted, decodeErr := codec.Unmarshal(backend.file(w.mcpFile))
		if decodeErr != nil || len(persisted) != 1 || persisted[0].Name != "two" {
			t.Fatalf("reconciled MCP file = %#v, %v", persisted, decodeErr)
		}
	})
}

func TestWorkspaceHelpers(t *testing.T) {
	t.Parallel()

	config := httpMCPConfig("clone")
	config.HTTP.Headers = map[string]string{"Authorization": "secret"}
	config.EnabledTools = []string{"one"}
	client := &fakeMCP{config: config}
	cloned, err := mcpConfig(client)
	if err != nil {
		t.Fatalf("mcpConfig returned error: %v", err)
	}
	cloned.HTTP.Headers["Authorization"] = "changed"
	cloned.EnabledTools[0] = "changed"
	if client.config.HTTP.Headers["Authorization"] != "secret" || client.config.EnabledTools[0] != "one" {
		t.Fatal("mcpConfig must return a deep copy")
	}
	stdioOriginal := workspace.MCPClientConfig{
		Name: "stdio", Type: workspace.MCPClientTypeStdio, Stateful: true,
		Stdio:         &workspace.MCPStdioConfig{Command: "server", Args: []string{"one"}, Env: map[string]string{"KEY": "value"}},
		DisabledTools: []string{"skip"},
	}
	stdioClone := cloneMCPClientConfig(stdioOriginal)
	stdioClone.Stdio.Args[0] = "changed"
	stdioClone.Stdio.Env["KEY"] = "changed"
	stdioClone.DisabledTools[0] = "changed"
	if stdioOriginal.Stdio.Args[0] != "one" || stdioOriginal.Stdio.Env["KEY"] != "value" || stdioOriginal.DisabledTools[0] != "skip" {
		t.Fatal("cloneMCPClientConfig must deep-copy stdio fields")
	}
	for _, current := range []workspace.MCPClient{nil, &plainMCP{name: "plain"}} {
		if _, err := mcpConfig(current); err == nil {
			t.Fatalf("mcpConfig(%#v) should fail", current)
		}
	}
	if _, err := mcpConfig(&fakeMCP{configErr: errors.New("config failed")}); err == nil {
		t.Fatal("mcpConfig should return provider error")
	}
	_, err = mcpConfig(&fakeMCP{name: "runtime", config: httpMCPConfig("persisted")})
	requireErrorContains(t, err, "does not match persisted name")
	if cloneStringMap(nil) != nil || cloneStringMap(map[string]string{}) != nil {
		t.Fatal("cloneStringMap should preserve empty as nil")
	}
	values := map[string]string{"a": "b"}
	copyValues := cloneStringMap(values)
	copyValues["a"] = "c"
	if values["a"] != "b" {
		t.Fatal("cloneStringMap must clone values")
	}

	for _, test := range []struct {
		value string
		want  string
		err   bool
	}{
		{value: " /work/../root ", want: "/root"},
		{value: "", err: true},
		{value: "relative", err: true},
		{value: "/", err: true},
		{value: "/bad\x00path", err: true},
	} {
		got, err := absoluteSandboxPath(test.value)
		if test.err && err == nil || !test.err && (err != nil || got != test.want) {
			t.Fatalf("absoluteSandboxPath(%q) = %q, %v", test.value, got, err)
		}
	}
	commands := [][]string{{"one", "two"}}
	clonedCommands := cloneArgvList(commands)
	commands[0][0] = "changed"
	if clonedCommands[0][0] != "one" || cloneArgvList(nil) != nil {
		t.Fatal("cloneArgvList did not clone commands")
	}

	if !(ExecResult{ExitCode: 0}).OK() || (ExecResult{ExitCode: 1}).OK() {
		t.Fatal("ExecResult.OK returned unexpected result")
	}
	err = commandError("run", ExecResult{ExitCode: 2, Stderr: []byte(" stderr ")})
	requireErrorContains(t, err, "stderr")
	err = commandError("run", ExecResult{ExitCode: 3, Stdout: []byte(strings.Repeat("x", 2100))})
	if len(err.Error()) > 2100 || !strings.Contains(err.Error(), "exit code 3") {
		t.Fatalf("commandError did not bound output: %d %v", len(err.Error()), err)
	}

	w, backend, _, _, _ := readyWorkspace(t)
	backend.files[w.gatewayLog] = []byte("0123456789")
	if got := w.gatewayLogTail(context.Background(), 4); got != "6789" || w.gatewayLogTail(context.Background(), 0) != "0123456789" {
		t.Fatal("gatewayLogTail returned unexpected tail")
	}
	for _, exitCode := range []int{0, 1, 2} {
		backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
			if len(argv) > 0 && argv[0] == "test" {
				return ExecResult{ExitCode: exitCode, Stderr: []byte("test error")}, nil, true
			}
			return ExecResult{}, nil, false
		}
		exists, err := w.fileExists(context.Background(), "/x")
		if exitCode == 0 && (!exists || err != nil) || exitCode == 1 && (exists || err != nil) || exitCode == 2 && err == nil {
			t.Fatalf("fileExists exit %d = %t, %v", exitCode, exists, err)
		}
	}
	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{}, errors.New("exec failed"), true
	}
	if _, err := w.fileExists(context.Background(), "/x"); err == nil {
		t.Fatal("fileExists should wrap exec error")
	}
}

func httpMCPConfig(name string) workspace.MCPClientConfig {
	return workspace.MCPClientConfig{
		Name: name,
		Type: workspace.MCPClientTypeHTTP,
		HTTP: &workspace.MCPHTTPConfig{URL: "https://example.test/" + name},
	}
}

func assertMCPSnapshots(
	t *testing.T,
	w *Workspace,
	backend *fakeBackend,
	gatewayClient *fakeGateway,
	wantLocalNames []string,
	wantGatewayNames []string,
	wantFile []byte,
) {
	t.Helper()
	if len(w.mcps) != len(wantLocalNames) {
		t.Fatalf("local MCP count = %d, want %d: %#v", len(w.mcps), len(wantLocalNames), w.mcps)
	}
	for index, name := range wantLocalNames {
		if w.mcps[index] == nil || w.mcps[index].Name() != name {
			t.Fatalf("local MCP %d = %#v, want %q", index, w.mcps[index], name)
		}
	}
	configs, err := gatewayClient.ListMCPs(context.Background())
	if err != nil {
		t.Fatalf("ListMCPs returned error: %v", err)
	}
	if len(configs) != len(wantGatewayNames) {
		t.Fatalf("gateway MCP count = %d, want %d: %#v", len(configs), len(wantGatewayNames), configs)
	}
	wantNames := make(map[string]int, len(wantGatewayNames))
	for _, name := range wantGatewayNames {
		wantNames[name]++
	}
	for _, config := range configs {
		wantNames[config.Name]--
	}
	for name, count := range wantNames {
		if count != 0 {
			t.Fatalf("gateway MCP snapshot differs for %q: delta=%d configs=%#v", name, count, configs)
		}
	}
	if string(backend.file(w.mcpFile)) != string(wantFile) {
		t.Fatalf("MCP file = %q, want %q", backend.file(w.mcpFile), wantFile)
	}
}
