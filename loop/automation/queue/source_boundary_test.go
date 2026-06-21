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

func TestMessageAndAdapterBoundaryErrors(t *testing.T) {
	t.Parallel()

	if err := (queue.Message{}).Ack(context.Background()); err != nil {
		t.Fatalf("message without AckFunc should ack successfully: %v", err)
	}
	if err := (queue.Message{}).Nack(context.Background(), errors.New("ignored")); err != nil {
		t.Fatalf("message without NackFunc should nack successfully: %v", err)
	}
	if _, err := (queue.ReceiverFunc)(nil).Receive(context.Background()); err == nil {
		t.Fatalf("nil ReceiverFunc should fail")
	}
	if _, err := (queue.DecoderFunc)(nil).Decode(context.Background(), queue.Message{}); err == nil {
		t.Fatalf("nil DecoderFunc should fail")
	}

	ackErr := errors.New("ack failed")
	nackErr := errors.New("nack failed")
	message := queue.Message{
		AckFunc: func(context.Context) error {
			return ackErr
		},
		NackFunc: func(context.Context, error) error {
			return nackErr
		},
	}
	if err := message.Ack(context.Background()); !errors.Is(err, ackErr) {
		t.Fatalf("Ack error = %v, want %v", err, ackErr)
	}
	if err := message.Nack(context.Background(), errors.New("cause")); !errors.Is(err, nackErr) {
		t.Fatalf("Nack error = %v, want %v", err, nackErr)
	}
}

func TestSourceStartRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	handler := eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error { return nil })
	receiver := queue.ReceiverFunc(func(context.Context) (queue.Message, error) { return queue.Message{}, context.Canceled })
	decoder := queue.DecoderFunc(func(context.Context, queue.Message) (eventpkg.Event, error) {
		return eventpkg.Event{ID: "evt-1", Source: "queue://test", Type: "queue.message"}, nil
	})

	tests := []struct {
		name    string
		ctx     context.Context
		source  queue.Source
		handler eventpkg.EventHandler
	}{
		{name: "nil context", ctx: nil, source: queue.Source{Receiver: receiver, Decoder: decoder}, handler: handler},
		{name: "nil handler", ctx: context.Background(), source: queue.Source{Receiver: receiver, Decoder: decoder}},
		{name: "nil receiver", ctx: context.Background(), source: queue.Source{Decoder: decoder}, handler: handler},
		{name: "nil decoder", ctx: context.Background(), source: queue.Source{Receiver: receiver}, handler: handler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.source.Start(tt.ctx, tt.handler); err == nil {
				t.Fatalf("Start should reject %s", tt.name)
			}
		})
	}
}

func TestSourceStartReturnsAckErrorAfterSuccessfulHandling(t *testing.T) {
	t.Parallel()

	ackErr := errors.New("broker ack failed")
	source := queue.Source{
		Receiver: queue.ReceiverFunc(func(context.Context) (queue.Message, error) {
			return queue.Message{
				ID: "msg-ack",
				AckFunc: func(context.Context) error {
					return ackErr
				},
				NackFunc: func(context.Context, error) error {
					t.Fatalf("message should not be nacked after handler success")
					return nil
				},
			}, nil
		}),
		Decoder: queue.DecoderFunc(func(context.Context, queue.Message) (eventpkg.Event, error) {
			return eventpkg.Event{ID: "evt-ack", Source: "queue://test", Type: "queue.message"}, nil
		}),
	}

	err := source.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return nil
	}))

	if !errors.Is(err, ackErr) {
		t.Fatalf("Start ack error = %v, want %v", err, ackErr)
	}
}

func TestSourceDecoderReceivesClonedMessage(t *testing.T) {
	t.Parallel()

	originalAttributes := map[string]string{"trace": "original"}
	source := queue.Source{
		Receiver: queue.ReceiverFunc(func(context.Context) (queue.Message, error) {
			return queue.Message{
				ID:         "msg-clone",
				Body:       []byte("payload"),
				Attributes: originalAttributes,
			}, nil
		}),
		Decoder: queue.DecoderFunc(func(_ context.Context, message queue.Message) (eventpkg.Event, error) {
			message.Body[0] = 'P'
			message.Attributes["trace"] = "mutated"
			return eventpkg.Event{ID: "evt-clone", Source: "queue://test", Type: "queue.message"}, nil
		}),
	}

	err := source.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return errors.New("stop after decode")
	}))

	if err == nil {
		t.Fatalf("Start should stop after the handler error")
	}
	if originalAttributes["trace"] != "original" {
		t.Fatalf("decoder should receive cloned attributes, got %#v", originalAttributes)
	}
}
