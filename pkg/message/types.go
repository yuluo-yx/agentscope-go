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

package message

import (
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
)

type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	ID   string `json:"id"`
}

type ThinkingBlock struct {
	Type     string         `json:"type"`
	Thinking string         `json:"thinking"`
	ID       string         `json:"id"`
	Extra    map[string]any `json:"-"`
}

type HintBlock struct {
	Type   string           `json:"type"`
	Hint   string           `json:"-"`
	Blocks ContentBlockList `json:"-"`
	ID     string           `json:"id"`
	Source *string          `json:"source,omitempty"`
}

type ToolCallState string

const (
	ToolCallPending   ToolCallState = "pending"
	ToolCallAsking    ToolCallState = "asking"
	ToolCallAllowed   ToolCallState = "allowed"
	ToolCallSubmitted ToolCallState = "submitted"
	ToolCallFinished  ToolCallState = "finished"
)

type ToolCallBlock struct {
	Type           string            `json:"type"`
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Input          string            `json:"input"`
	State          ToolCallState     `json:"state"`
	SuggestedRules []permission.Rule `json:"suggested_rules"`
	Extra          map[string]any    `json:"-"`
}

type ToolResultState string

const (
	ToolResultSuccess     ToolResultState = "success"
	ToolResultError       ToolResultState = "error"
	ToolResultInterrupted ToolResultState = "interrupted"
	ToolResultDenied      ToolResultState = "denied"
	ToolResultRunning     ToolResultState = "running"
)

type ToolResultBlock struct {
	Type   string           `json:"type"`
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Output ToolResultOutput `json:"output"`
	State  ToolResultState  `json:"state"`
}

type ToolResultOutput struct {
	Raw    string
	Blocks ContentBlockList
}

type DataBlock struct {
	Type   string     `json:"type"`
	ID     string     `json:"id"`
	Source DataSource `json:"source"`
	Name   *string    `json:"name"`
}
