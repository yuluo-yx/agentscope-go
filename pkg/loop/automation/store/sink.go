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

package store

import (
	"context"
	"errors"
	"fmt"
)

// Sink publishes loop run results to an external or application-defined
// destination, such as a file, ticket comment, message, database, or audit log.
type Sink interface {
	PublishRun(context.Context, RunRecord, LoopReport) error
}

// SinkFunc adapts a function to Sink.
type SinkFunc func(context.Context, RunRecord, LoopReport) error

// PublishRun calls f(ctx, run, report).
func (f SinkFunc) PublishRun(ctx context.Context, run RunRecord, report LoopReport) error {
	if f == nil {
		return fmt.Errorf("automation: sink is nil")
	}
	return f(ctx, cloneRunRecord(run), CloneLoopReport(report))
}

// MultiSink publishes to each configured sink and joins returned errors.
type MultiSink []Sink

// PublishRun publishes run and report to every sink.
func (s MultiSink) PublishRun(ctx context.Context, run RunRecord, report LoopReport) error {
	var errs []error
	for _, sink := range s {
		if sink == nil {
			continue
		}
		if err := sink.PublishRun(ctx, cloneRunRecord(run), CloneLoopReport(report)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
