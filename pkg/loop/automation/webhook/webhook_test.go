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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	eventpkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/automation/webhook"
)

func TestSourceHandlerVerifiesDecodesAndDispatchesEvent(t *testing.T) {
	t.Parallel()

	var got eventpkg.Event
	verified := false
	source := webhook.Source{
		Decoder: webhook.DecoderFunc(func(request *http.Request) (eventpkg.Event, error) {
			if request.Header.Get("X-Webhook") != "test" {
				t.Fatalf("decoder did not receive request headers")
			}
			return eventpkg.Event{
				ID:     "evt-1",
				Source: "webhook://test",
				Type:   "webhook.received",
				Labels: []string{"webhook"},
			}, nil
		}),
		Verifier: webhook.VerifierFunc(func(request *http.Request) error {
			verified = true
			if request.Header.Get("X-Signature") != "ok" {
				return errors.New("bad signature")
			}
			return nil
		}),
		SuccessStatus: http.StatusCreated,
	}
	handler := source.Handler(eventpkg.EventHandlerFunc(func(_ context.Context, event eventpkg.Event) error {
		got = event
		return nil
	}))
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"ok":true}`))
	request.Header.Set("X-Webhook", "test")
	request.Header.Set("X-Signature", "ok")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusCreated)
	}
	if !verified {
		t.Fatalf("verifier should be called")
	}
	if got.ID != "evt-1" || got.Source != "webhook://test" || got.Type != "webhook.received" {
		t.Fatalf("dispatched event mismatch: %#v", got)
	}
}

func TestSourceHandlerRejectsVerifierFailureBeforeDecode(t *testing.T) {
	t.Parallel()

	decoded := false
	source := webhook.Source{
		Decoder: webhook.DecoderFunc(func(*http.Request) (eventpkg.Event, error) {
			decoded = true
			return eventpkg.Event{}, nil
		}),
		Verifier: webhook.VerifierFunc(func(*http.Request) error {
			return errors.New("signature mismatch")
		}),
	}
	handler := source.Handler(eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		t.Fatalf("handler should not be called after verifier failure")
		return nil
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/webhook", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if decoded {
		t.Fatalf("decoder should not run after verifier failure")
	}
}

func TestSourceHandlerReportsDecodeAndDispatchErrors(t *testing.T) {
	t.Parallel()

	decodeSource := webhook.Source{
		Decoder: webhook.DecoderFunc(func(*http.Request) (eventpkg.Event, error) {
			return eventpkg.Event{}, errors.New("bad payload")
		}),
	}
	decodeResponse := httptest.NewRecorder()
	decodeSource.Handler(eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		t.Fatalf("handler should not be called after decode failure")
		return nil
	})).ServeHTTP(decodeResponse, httptest.NewRequest(http.MethodPost, "/webhook", nil))
	if decodeResponse.Code != http.StatusBadRequest {
		t.Fatalf("decode response status = %d, want %d", decodeResponse.Code, http.StatusBadRequest)
	}

	dispatchSource := webhook.Source{
		Decoder: webhook.DecoderFunc(func(*http.Request) (eventpkg.Event, error) {
			return eventpkg.Event{ID: "evt-2", Source: "webhook://test", Type: "webhook.received"}, nil
		}),
	}
	dispatchResponse := httptest.NewRecorder()
	dispatchSource.Handler(eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return errors.New("runner failed")
	})).ServeHTTP(dispatchResponse, httptest.NewRequest(http.MethodPost, "/webhook", nil))
	if dispatchResponse.Code != http.StatusInternalServerError {
		t.Fatalf("dispatch response status = %d, want %d", dispatchResponse.Code, http.StatusInternalServerError)
	}
}

func TestSourceHandlerReportsConfigurationAndValidationErrors(t *testing.T) {
	t.Parallel()

	nilDecoderResponse := httptest.NewRecorder()
	webhook.Source{}.Handler(eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		t.Fatalf("handler should not be called without decoder")
		return nil
	})).ServeHTTP(nilDecoderResponse, httptest.NewRequest(http.MethodPost, "/webhook", nil))
	if nilDecoderResponse.Code != http.StatusInternalServerError {
		t.Fatalf("nil decoder response status = %d, want %d", nilDecoderResponse.Code, http.StatusInternalServerError)
	}

	invalidEventSource := webhook.Source{
		Decoder: webhook.DecoderFunc(func(*http.Request) (eventpkg.Event, error) {
			return eventpkg.Event{ID: "evt-invalid"}, nil
		}),
	}
	invalidEventResponse := httptest.NewRecorder()
	invalidEventSource.Handler(eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		t.Fatalf("handler should not be called for invalid event")
		return nil
	})).ServeHTTP(invalidEventResponse, httptest.NewRequest(http.MethodPost, "/webhook", nil))
	if invalidEventResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid event response status = %d, want %d", invalidEventResponse.Code, http.StatusBadRequest)
	}
}

func TestSourceHandlerEnforcesMethodAndDefaultStatus(t *testing.T) {
	t.Parallel()

	source := webhook.Source{
		Method: http.MethodPut,
		Decoder: webhook.DecoderFunc(func(*http.Request) (eventpkg.Event, error) {
			return eventpkg.Event{ID: "evt-3", Source: "webhook://test", Type: "webhook.received"}, nil
		}),
	}
	handler := source.Handler(eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return nil
	}))

	wrongMethodResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongMethodResponse, httptest.NewRequest(http.MethodPost, "/webhook", nil))
	if wrongMethodResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method response status = %d, want %d", wrongMethodResponse.Code, http.StatusMethodNotAllowed)
	}
	if allow := wrongMethodResponse.Header().Get("Allow"); allow != http.MethodPut {
		t.Fatalf("allow header = %q, want %q", allow, http.MethodPut)
	}

	successResponse := httptest.NewRecorder()
	handler.ServeHTTP(successResponse, httptest.NewRequest(http.MethodPut, "/webhook", nil))
	if successResponse.Code != http.StatusAccepted {
		t.Fatalf("default success status = %d, want %d", successResponse.Code, http.StatusAccepted)
	}
}

func TestSourceHandlerRejectsInvalidSuccessStatus(t *testing.T) {
	t.Parallel()

	source := webhook.Source{
		SuccessStatus: 42,
		Decoder: webhook.DecoderFunc(func(*http.Request) (eventpkg.Event, error) {
			return eventpkg.Event{ID: "evt-4", Source: "webhook://test", Type: "webhook.received"}, nil
		}),
	}
	response := httptest.NewRecorder()

	source.Handler(eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return nil
	})).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/webhook", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("invalid success status response = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
