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

// Package webhook provides a generic HTTP webhook source for automation events.
package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/loop/automation/event"
)

// Decoder converts an HTTP request into a generic automation event.
type Decoder interface {
	Decode(*http.Request) (event.Event, error)
}

// DecoderFunc adapts a function to Decoder.
type DecoderFunc func(*http.Request) (event.Event, error)

// Decode calls f(request).
func (f DecoderFunc) Decode(request *http.Request) (event.Event, error) {
	if f == nil {
		return event.Event{}, fmt.Errorf("webhook: decoder is nil")
	}
	return f(request)
}

// Verifier checks a request before it is decoded.
type Verifier interface {
	Verify(*http.Request) error
}

// VerifierFunc adapts a function to Verifier.
type VerifierFunc func(*http.Request) error

// Verify calls f(request).
func (f VerifierFunc) Verify(request *http.Request) error {
	if f == nil {
		return fmt.Errorf("webhook: verifier is nil")
	}
	return f(request)
}

// Source receives HTTP webhook requests and dispatches decoded events.
type Source struct {
	Addr          string
	Decoder       Decoder
	Verifier      Verifier
	Method        string
	SuccessStatus int
	Server        *http.Server
}

// Handler returns an http.Handler that verifies, decodes, validates, and
// dispatches one webhook request.
func (s Source) Handler(handler event.EventHandler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if handler == nil {
			http.Error(response, "webhook: event handler is nil", http.StatusInternalServerError)
			return
		}
		method := strings.TrimSpace(s.Method)
		if method == "" {
			method = http.MethodPost
		}
		if request.Method != method {
			response.Header().Set("Allow", method)
			http.Error(response, "webhook: method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.Verifier != nil {
			if err := s.Verifier.Verify(request); err != nil {
				http.Error(response, err.Error(), http.StatusUnauthorized)
				return
			}
		}
		if s.Decoder == nil {
			http.Error(response, "webhook: decoder is nil", http.StatusInternalServerError)
			return
		}
		event, err := s.Decoder.Decode(request)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		if err := event.Validate(); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		if err := handler.HandleEvent(request.Context(), event); err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		status := s.SuccessStatus
		if status == 0 {
			status = http.StatusAccepted
		}
		if status < 100 || status > 999 {
			http.Error(response, "webhook: success status is invalid", http.StatusInternalServerError)
			return
		}
		response.WriteHeader(status)
	})
}

// Start serves HTTP webhook requests until ctx is canceled.
func (s Source) Start(ctx context.Context, handler event.EventHandler) error {
	if ctx == nil {
		return fmt.Errorf("webhook: context is nil")
	}
	if handler == nil {
		return fmt.Errorf("webhook: event handler is nil")
	}
	server := s.Server
	if server == nil {
		addr := strings.TrimSpace(s.Addr)
		if addr == "" {
			return fmt.Errorf("webhook: address is empty")
		}
		server = &http.Server{Addr: addr, ReadHeaderTimeout: 5 * time.Second}
	}
	server.Handler = s.Handler(handler)

	errCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		if err := server.Shutdown(context.WithoutCancel(ctx)); err != nil {
			return err
		}
		return nil
	}
}

var _ event.EventSource = Source{}
