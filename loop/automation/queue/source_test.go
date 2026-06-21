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

package queue_test

import (
	"context"
	"errors"
	"testing"

	eventpkg "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	"github.com/yuluo-yx/agentscope-go/loop/automation/queue"
)

func TestSourceStartDecodesHandlesAndAcksMessage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := 0
	acked := false
	nacked := false
	source := queue.Source{
		Receiver: queue.ReceiverFunc(func(ctx context.Context) (queue.Message, error) {
			received++
			if received > 1 {
				<-ctx.Done()
				return queue.Message{}, ctx.Err()
			}
			return queue.Message{
				ID:   "msg-1",
				Body: []byte(`{"id":"evt-1"}`),
				AckFunc: func(context.Context) error {
					acked = true
					return nil
				},
				NackFunc: func(context.Context, error) error {
					nacked = true
					return nil
				},
			}, nil
		}),
		Decoder: queue.DecoderFunc(func(_ context.Context, message queue.Message) (eventpkg.Event, error) {
			if string(message.Body) != `{"id":"evt-1"}` {
				t.Fatalf("message body = %q", string(message.Body))
			}
			return eventpkg.Event{
				ID:     "evt-1",
				Source: "queue://test",
				Type:   "queue.message",
			}, nil
		}),
	}

	err := source.Start(ctx, eventpkg.EventHandlerFunc(func(_ context.Context, event eventpkg.Event) error {
		if event.ID != "evt-1" || event.Source != "queue://test" || event.Type != "queue.message" {
			t.Fatalf("event mismatch: %#v", event)
		}
		cancel()
		return nil
	}))
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !acked {
		t.Fatalf("message should be acked after successful handling")
	}
	if nacked {
		t.Fatalf("message should not be nacked after successful handling")
	}
}

func TestSourceStartNacksDecodeAndValidationErrors(t *testing.T) {
	t.Parallel()

	decodeErr := errors.New("bad payload")
	nackedDecode := false
	decodeSource := queue.Source{
		Receiver: queue.ReceiverFunc(func(context.Context) (queue.Message, error) {
			return queue.Message{
				ID: "msg-decode",
				NackFunc: func(_ context.Context, cause error) error {
					nackedDecode = errors.Is(cause, decodeErr)
					return nil
				},
			}, nil
		}),
		Decoder: queue.DecoderFunc(func(context.Context, queue.Message) (eventpkg.Event, error) {
			return eventpkg.Event{}, decodeErr
		}),
	}
	err := decodeSource.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		t.Fatalf("handler should not run after decode failure")
		return nil
	}))
	if !errors.Is(err, decodeErr) {
		t.Fatalf("decode error = %v, want %v", err, decodeErr)
	}
	if !nackedDecode {
		t.Fatalf("message should be nacked with decode error")
	}

	nackedInvalid := false
	invalidSource := queue.Source{
		Receiver: queue.ReceiverFunc(func(context.Context) (queue.Message, error) {
			return queue.Message{
				ID: "msg-invalid",
				NackFunc: func(_ context.Context, cause error) error {
					nackedInvalid = cause != nil
					return nil
				},
			}, nil
		}),
		Decoder: queue.DecoderFunc(func(context.Context, queue.Message) (eventpkg.Event, error) {
			return eventpkg.Event{ID: "evt-invalid"}, nil
		}),
	}
	err = invalidSource.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		t.Fatalf("handler should not run for invalid event")
		return nil
	}))
	if err == nil {
		t.Fatalf("Start should return validation error")
	}
	if !nackedInvalid {
		t.Fatalf("message should be nacked after event validation failure")
	}
}

func TestSourceStartNacksHandlerError(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("runner failed")
	acked := false
	nacked := false
	source := queue.Source{
		Receiver: queue.ReceiverFunc(func(context.Context) (queue.Message, error) {
			return queue.Message{
				ID: "msg-handler",
				AckFunc: func(context.Context) error {
					acked = true
					return nil
				},
				NackFunc: func(_ context.Context, cause error) error {
					nacked = errors.Is(cause, handlerErr)
					return nil
				},
			}, nil
		}),
		Decoder: queue.DecoderFunc(func(context.Context, queue.Message) (eventpkg.Event, error) {
			return eventpkg.Event{ID: "evt-2", Source: "queue://test", Type: "queue.message"}, nil
		}),
	}

	err := source.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return handlerErr
	}))

	if !errors.Is(err, handlerErr) {
		t.Fatalf("handler error = %v, want %v", err, handlerErr)
	}
	if acked {
		t.Fatalf("message should not be acked after handler error")
	}
	if !nacked {
		t.Fatalf("message should be nacked with handler error")
	}
}

func TestSourceStartStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := queue.Source{
		Receiver: queue.ReceiverFunc(func(ctx context.Context) (queue.Message, error) {
			return queue.Message{}, ctx.Err()
		}),
		Decoder: queue.DecoderFunc(func(context.Context, queue.Message) (eventpkg.Event, error) {
			t.Fatalf("decoder should not run when source context is canceled")
			return eventpkg.Event{}, nil
		}),
	}

	err := source.Start(ctx, eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		t.Fatalf("handler should not run when source context is canceled")
		return nil
	}))
	if err != nil {
		t.Fatalf("Start returned error after context cancellation: %v", err)
	}
}

func TestSourceStartReturnsReceiverErrorWhenContextIsActive(t *testing.T) {
	t.Parallel()

	receiverErr := context.Canceled
	source := queue.Source{
		Receiver: queue.ReceiverFunc(func(context.Context) (queue.Message, error) {
			return queue.Message{}, receiverErr
		}),
		Decoder: queue.DecoderFunc(func(context.Context, queue.Message) (eventpkg.Event, error) {
			t.Fatalf("decoder should not run after receiver error")
			return eventpkg.Event{}, nil
		}),
	}

	err := source.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		t.Fatalf("handler should not run after receiver error")
		return nil
	}))

	if !errors.Is(err, receiverErr) {
		t.Fatalf("receiver error = %v, want %v", err, receiverErr)
	}
}
