// Copyright 20\d\d AgentScope Go
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
	"encoding/json"
	"fmt"
)

func (l ContentBlockList) MarshalJSON() ([]byte, error) {
	items := make([]json.RawMessage, 0, len(l))
	for _, block := range l {
		data, err := json.Marshal(block)
		if err != nil {
			return nil, err
		}
		items = append(items, data)
	}
	return json.Marshal(items)
}

func (l *ContentBlockList) UnmarshalJSON(data []byte) error {
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return err
	}
	blocks := make(ContentBlockList, 0, len(raws))
	for _, raw := range raws {
		block, err := UnmarshalContentBlock(raw)
		if err != nil {
			return err
		}
		blocks = append(blocks, block)
	}
	*l = blocks
	return nil
}

func UnmarshalContentBlock(data []byte) (ContentBlock, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	switch probe.Type {
	case "text":
		var block TextBlock
		return &block, json.Unmarshal(data, &block)
	case "thinking":
		var block ThinkingBlock
		return &block, json.Unmarshal(data, &block)
	case "hint":
		var block HintBlock
		return &block, json.Unmarshal(data, &block)
	case "tool_call":
		var block ToolCallBlock
		return &block, json.Unmarshal(data, &block)
	case "tool_result":
		var block ToolResultBlock
		return &block, json.Unmarshal(data, &block)
	case "data":
		var block DataBlock
		return &block, json.Unmarshal(data, &block)
	default:
		return nil, fmt.Errorf("message: unsupported content block type %q", probe.Type)
	}
}

func (b ThinkingBlock) MarshalJSON() ([]byte, error) {
	type alias ThinkingBlock
	base, err := marshalWithExtra(alias(b), b.Extra)
	if err != nil {
		return nil, err
	}
	return json.Marshal(base)
}

func (b *ThinkingBlock) UnmarshalJSON(data []byte) error {
	type alias ThinkingBlock
	var value alias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	deleteKnown(raw, "type", "thinking", "id")
	*b = ThinkingBlock(value)
	b.Extra = raw
	return nil
}

func (b ToolCallBlock) MarshalJSON() ([]byte, error) {
	type alias ToolCallBlock
	base, err := marshalWithExtra(alias(b), b.Extra)
	if err != nil {
		return nil, err
	}
	return json.Marshal(base)
}

func (b *ToolCallBlock) UnmarshalJSON(data []byte) error {
	type alias ToolCallBlock
	var value alias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	deleteKnown(raw, "type", "id", "name", "input", "state", "suggested_rules")
	*b = ToolCallBlock(value)
	b.Extra = raw
	if b.State == "" {
		b.State = ToolCallPending
	}
	return nil
}

func (b *DataBlock) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type   string          `json:"type"`
		ID     string          `json:"id"`
		Source json.RawMessage `json:"source"`
		Name   *string         `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	source, err := UnmarshalDataSource(raw.Source)
	if err != nil {
		return err
	}
	b.Type = raw.Type
	b.ID = raw.ID
	b.Source = source
	b.Name = raw.Name
	return nil
}

func UnmarshalDataSource(data []byte) (DataSource, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	switch probe.Type {
	case "base64":
		var source Base64Source
		return &source, json.Unmarshal(data, &source)
	case "url":
		var source URLSource
		return &source, json.Unmarshal(data, &source)
	default:
		return nil, fmt.Errorf("message: unsupported data source type %q", probe.Type)
	}
}

func (o ToolResultOutput) MarshalJSON() ([]byte, error) {
	if o.Blocks != nil {
		return json.Marshal(o.Blocks)
	}
	return json.Marshal(o.Raw)
}

func (o *ToolResultOutput) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &o.Raw)
	}
	var blocks ContentBlockList
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	o.Raw = ""
	o.Blocks = blocks
	return nil
}

func marshalWithExtra(value any, extra map[string]any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var base map[string]any
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, err
	}
	for k, v := range extra {
		base[k] = v
	}
	return base, nil
}

func deleteKnown(values map[string]any, keys ...string) {
	for _, key := range keys {
		delete(values, key)
	}
}
