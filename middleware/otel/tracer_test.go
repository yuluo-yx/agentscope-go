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

package otel

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracerStartsSpanAndRecordsAttributes(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := NewTracer(provider.Tracer("agentscope-test"))

	_, span := tracer.StartSpan(context.Background(), "reply", map[string]any{
		"string":       "value",
		"bool":         true,
		"int":          7,
		"int64":        int64(8),
		"float64":      1.5,
		"string_slice": []string{"a", "b"},
		"int_slice":    []int{1, 2},
		"int64_slice":  []int64{3, 4},
		"float_slice":  []float64{2.5, 3.5},
		"bool_slice":   []bool{true, false},
		"fallback":     struct{ Name string }{Name: "Friday"},
	})
	span.SetAttributes(map[string]any{"late": "attribute"})
	span.RecordError(errors.New("boom"))
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Name() != "reply" {
		t.Fatalf("ended span mismatch: %#v", ended)
	}
	attrs := ended[0].Attributes()
	if len(attrs) != 12 {
		t.Fatalf("attribute count mismatch: got %d attrs=%#v", len(attrs), attrs)
	}
	if len(ended[0].Events()) != 1 || ended[0].Events()[0].Name != "exception" {
		t.Fatalf("recorded error event mismatch: %#v", ended[0].Events())
	}
}

func TestTracerNilPathsAreNoops(t *testing.T) {
	t.Parallel()

	if NewTracer(nil).tracer == nil {
		t.Fatal("NewTracer(nil) should install the global OpenTelemetry tracer")
	}

	var tracer *Tracer
	_, span := tracer.StartSpan(context.Background(), "noop", map[string]any{"key": "value"})
	span.SetAttributes(map[string]any{"late": "attribute"})
	span.RecordError(errors.New("ignored"))
	span.End()

	otelSpan{}.SetAttributes(map[string]any{"ignored": true})
	otelSpan{}.RecordError(errors.New("ignored"))
	otelSpan{}.RecordError(nil)
	otelSpan{}.End()
}
