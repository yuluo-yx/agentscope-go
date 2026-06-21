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

// Package queue provides a generic queue-backed source for automation events.
package queue

import (
	"context"
	"errors"
	"fmt"

	"github.com/yuluo-yx/agentscope-go/loop/automation/event"
)

// Message is one broker message received by Source.
type Message struct {
	ID         string
	Body       []byte
	Attributes map[string]string
	AckFunc    func(context.Context) error
	NackFunc   func(context.Context, error) error
}

// Ack acknowledges successful processing of the message.
func (m Message) Ack(ctx context.Context) error {
	if m.AckFunc == nil {
		return nil
	}
	return m.AckFunc(ctx)
}

// Nack rejects failed processing of the message.
func (m Message) Nack(ctx context.Context, cause error) error {
	if m.NackFunc == nil {
		return nil
	}
	return m.NackFunc(ctx, cause)
}

func (m Message) clone() Message {
	cp := m
	cp.Body = append([]byte(nil), m.Body...)
	if m.Attributes != nil {
		cp.Attributes = make(map[string]string, len(m.Attributes))
		for key, value := range m.Attributes {
			cp.Attributes[key] = value
		}
	}
	return cp
}

// Receiver receives one queue message. Implementations should return ctx.Err()
// when ctx is canceled.
type Receiver interface {
	Receive(context.Context) (Message, error)
}

// ReceiverFunc adapts a function to Receiver.
type ReceiverFunc func(context.Context) (Message, error)

// Receive calls f(ctx).
func (f ReceiverFunc) Receive(ctx context.Context) (Message, error) {
	if f == nil {
		return Message{}, fmt.Errorf("queue: receiver is nil")
	}
	return f(ctx)
}

// Decoder converts a queue message into a generic automation event.
type Decoder interface {
	Decode(context.Context, Message) (event.Event, error)
}

// DecoderFunc adapts a function to Decoder.
type DecoderFunc func(context.Context, Message) (event.Event, error)

// Decode calls f(ctx, message).
func (f DecoderFunc) Decode(ctx context.Context, message Message) (event.Event, error) {
	if f == nil {
		return event.Event{}, fmt.Errorf("queue: decoder is nil")
	}
	return f(ctx, message)
}

// Source receives queue messages and dispatches decoded automation events.
type Source struct {
	Receiver Receiver
	Decoder  Decoder
}

// Start receives messages until ctx is canceled or processing fails.
func (s Source) Start(ctx context.Context, handler event.EventHandler) error {
	if ctx == nil {
		return fmt.Errorf("queue: context is nil")
	}
	if handler == nil {
		return fmt.Errorf("queue: event handler is nil")
	}
	if s.Receiver == nil {
		return fmt.Errorf("queue: receiver is nil")
	}
	if s.Decoder == nil {
		return fmt.Errorf("queue: decoder is nil")
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		message, err := s.Receiver.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := s.handleMessage(ctx, handler, message); err != nil {
			return err
		}
	}
}

func (s Source) handleMessage(ctx context.Context, handler event.EventHandler, message Message) error {
	evt, err := s.Decoder.Decode(ctx, message.clone())
	if err != nil {
		return rejectMessage(ctx, message, err)
	}
	if err := evt.Validate(); err != nil {
		return rejectMessage(ctx, message, err)
	}
	if err := handler.HandleEvent(ctx, evt); err != nil {
		return rejectMessage(ctx, message, err)
	}
	if err := message.Ack(context.WithoutCancel(ctx)); err != nil {
		return err
	}
	return nil
}

func rejectMessage(ctx context.Context, message Message, cause error) error {
	return errors.Join(cause, message.Nack(context.WithoutCancel(ctx), cause))
}

var _ event.EventSource = Source{}
