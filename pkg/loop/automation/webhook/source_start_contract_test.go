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

package webhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	eventpkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/automation/webhook"
)

func TestDecoderVerifierAndHandlerNilBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := (webhook.DecoderFunc(nil)).Decode(httptest.NewRequest(http.MethodPost, "/webhook", nil)); err == nil ||
		!strings.Contains(err.Error(), "decoder is nil") {
		t.Fatalf("nil DecoderFunc error = %v, want decoder is nil", err)
	}
	if err := (webhook.VerifierFunc(nil)).Verify(httptest.NewRequest(http.MethodPost, "/webhook", nil)); err == nil ||
		!strings.Contains(err.Error(), "verifier is nil") {
		t.Fatalf("nil VerifierFunc error = %v, want verifier is nil", err)
	}

	response := httptest.NewRecorder()
	webhook.Source{}.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/webhook", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("nil handler response status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestSourceStartRejectsInvalidConfigurationBeforeServing(t *testing.T) {
	t.Parallel()

	var nilCtx context.Context
	err := webhook.Source{}.Start(nilCtx, eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error { return nil }))
	if err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("Start(nil) error = %v, want context is nil", err)
	}
	err = webhook.Source{}.Start(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "event handler is nil") {
		t.Fatalf("Start nil handler error = %v, want event handler is nil", err)
	}
	err = webhook.Source{Addr: " "}.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error { return nil }))
	if err == nil || !strings.Contains(err.Error(), "address is empty") {
		t.Fatalf("Start empty address error = %v, want address is empty", err)
	}
}

func TestSourceStartReturnsListenErrorFromConfiguredServer(t *testing.T) {
	t.Parallel()

	err := webhook.Source{
		Server: &http.Server{Addr: "127.0.0.1:notaport"},
	}.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return nil
	}))
	if err == nil {
		t.Fatalf("Start should return the server listen error")
	}
}

func TestSourceStartShutsDownConfiguredServerOnContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()

	err := webhook.Source{
		Server: &http.Server{Addr: "127.0.0.1:0"},
		Decoder: webhook.DecoderFunc(func(*http.Request) (eventpkg.Event, error) {
			return eventpkg.Event{ID: "evt-1", Source: "webhook://test", Type: "webhook.received"}, nil
		}),
	}.Start(ctx, eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return nil
	}))
	if err != nil {
		t.Fatalf("Start returned error during context shutdown: %v", err)
	}
}
