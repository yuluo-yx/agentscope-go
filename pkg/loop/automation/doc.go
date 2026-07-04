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

// Package automation provides event-driven outer orchestration for Loop Engineering.
//
// This package sits above package loop. Package loop controls one Agent run,
// while this package decides which generic event triggers that run, how events
// map to Agent input, how concurrency, budget, and estimated cost are controlled,
// how goals continue across runs, and how events, runs, and reports are audited.
// LoopTemplate describes reusable loop configuration and project knowledge
// references without binding to a concrete plugin format. Cost estimates are
// integrated only through the generic CostEstimator; model pricing tables and
// billing semantics are provided by application adapters.
package automation
