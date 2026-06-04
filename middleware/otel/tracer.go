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

// Package otel adapts OpenTelemetry tracers to middleware.Tracer.
package otel

import (
	"context"
	"fmt"

	"github.com/yuluo-yx/agentscope-go/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Tracer adapts an OpenTelemetry tracer to middleware.Tracer.
type Tracer struct {
	tracer trace.Tracer
}

// NewTracer creates an OpenTelemetry-backed middleware tracer.
func NewTracer(tracer trace.Tracer) *Tracer {
	if tracer == nil {
		tracer = otel.Tracer("github.com/yuluo-yx/agentscope-go/middleware")
	}
	return &Tracer{tracer: tracer}
}

// StartSpan starts an OpenTelemetry span.
func (t *Tracer) StartSpan(ctx context.Context, name string, attributes map[string]any) (context.Context, middleware.TraceSpan) {
	if t == nil || t.tracer == nil {
		return ctx, noopSpan{}
	}
	ctx, span := t.tracer.Start(ctx, name, trace.WithAttributes(attributePairs(attributes)...))
	return ctx, otelSpan{span: span}
}

type otelSpan struct {
	span trace.Span
}

func (s otelSpan) SetAttributes(attributes map[string]any) {
	if s.span == nil {
		return
	}
	s.span.SetAttributes(attributePairs(attributes)...)
}

func (s otelSpan) RecordError(err error) {
	if s.span == nil || err == nil {
		return
	}
	s.span.RecordError(err)
}

func (s otelSpan) End() {
	if s.span != nil {
		s.span.End()
	}
}

type noopSpan struct{}

func (noopSpan) SetAttributes(map[string]any) {}
func (noopSpan) RecordError(error)            {}
func (noopSpan) End()                         {}

func attributePairs(attributes map[string]any) []attribute.KeyValue {
	pairs := make([]attribute.KeyValue, 0, len(attributes))
	for key, value := range attributes {
		pairs = append(pairs, attributePair(key, value))
	}
	return pairs
}

func attributePair(key string, value any) attribute.KeyValue {
	switch typed := value.(type) {
	case string:
		return attribute.String(key, typed)
	case bool:
		return attribute.Bool(key, typed)
	case int:
		return attribute.Int(key, typed)
	case int64:
		return attribute.Int64(key, typed)
	case float64:
		return attribute.Float64(key, typed)
	case []string:
		return attribute.StringSlice(key, typed)
	case []int:
		return attribute.IntSlice(key, typed)
	case []int64:
		return attribute.Int64Slice(key, typed)
	case []float64:
		return attribute.Float64Slice(key, typed)
	case []bool:
		return attribute.BoolSlice(key, typed)
	default:
		return attribute.String(key, fmt.Sprint(value))
	}
}

var _ middleware.Tracer = (*Tracer)(nil)
