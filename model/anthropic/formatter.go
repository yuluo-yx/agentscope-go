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

package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

func formatMessages(messages []*message.Message) ([]sdk.TextBlockParam, []sdk.MessageParam, error) {
	system := []sdk.TextBlockParam{}
	formatted := make([]sdk.MessageParam, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		blocks, toolResults, err := formatContentBlocks(msg.Content)
		if err != nil {
			return nil, nil, err
		}
		if msg.Role == message.RoleSystem {
			system = append(system, systemBlocks(blocks)...)
			continue
		}
		if len(toolResults) > 0 {
			if len(blocks) > 0 {
				formatted = append(formatted, sdk.NewAssistantMessage(blocks...))
			}
			formatted = append(formatted, sdk.NewUserMessage(toolResults...))
			continue
		}
		switch msg.Role {
		case message.RoleUser:
			formatted = append(formatted, sdk.NewUserMessage(blocks...))
		case message.RoleAssistant:
			formatted = append(formatted, sdk.NewAssistantMessage(blocks...))
		default:
			return nil, nil, fmt.Errorf("anthropic: unsupported message role %q", msg.Role)
		}
	}
	return system, formatted, nil
}

func formatContentBlocks(blocks message.ContentBlockList) ([]sdk.ContentBlockParamUnion, []sdk.ContentBlockParamUnion, error) {
	content := make([]sdk.ContentBlockParamUnion, 0, len(blocks))
	toolResults := []sdk.ContentBlockParamUnion{}
	for _, block := range blocks {
		switch typed := block.(type) {
		case *message.TextBlock:
			content = append(content, sdk.NewTextBlock(typed.Text))
		case *message.HintBlock:
			hintBlocks, err := hintContentBlocks(typed)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, hintBlocks...)
		case *message.ThinkingBlock:
			part, err := thinkingBlockParam(typed)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, part)
		case *message.DataBlock:
			part, err := dataBlockParam(typed)
			if err != nil {
				return nil, nil, err
			}
			if part != nil {
				content = append(content, *part)
			}
		case *message.ToolCallBlock:
			input, err := toolCallInput(typed.Input)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, sdk.NewToolUseBlock(typed.ID, input, typed.Name))
		case *message.ToolResultBlock:
			toolResults = append(toolResults, sdk.NewToolResultBlock(typed.ID, toolResultText(typed.Output), typed.State != message.ToolResultSuccess))
		default:
			return nil, nil, fmt.Errorf("anthropic: unsupported content block %T", block)
		}
	}
	return content, toolResults, nil
}

func hintContentBlocks(block *message.HintBlock) ([]sdk.ContentBlockParamUnion, error) {
	if block.Blocks == nil {
		return []sdk.ContentBlockParamUnion{sdk.NewTextBlock(block.Hint)}, nil
	}
	content := make([]sdk.ContentBlockParamUnion, 0, len(block.Blocks))
	for _, nested := range block.Blocks {
		switch typed := nested.(type) {
		case *message.TextBlock:
			content = append(content, sdk.NewTextBlock(typed.Text))
		case *message.DataBlock:
			part, err := dataBlockParam(typed)
			if err != nil {
				return nil, err
			}
			if part != nil {
				content = append(content, *part)
			}
		default:
			return nil, fmt.Errorf("anthropic: unsupported hint content block %T", nested)
		}
	}
	return content, nil
}

func systemBlocks(blocks []sdk.ContentBlockParamUnion) []sdk.TextBlockParam {
	system := []sdk.TextBlockParam{}
	for _, block := range blocks {
		if text := block.GetText(); text != nil {
			system = append(system, sdk.TextBlockParam{Text: *text})
		}
	}
	return system
}

func thinkingBlockParam(block *message.ThinkingBlock) (sdk.ContentBlockParamUnion, error) {
	signature, _ := block.Extra["signature"].(string)
	if signature == "" {
		return sdk.ContentBlockParamUnion{}, fmt.Errorf("anthropic: thinking block requires signature extra")
	}
	return sdk.NewThinkingBlock(signature, block.Thinking), nil
}

func dataBlockParam(block *message.DataBlock) (*sdk.ContentBlockParamUnion, error) {
	if block == nil || block.Source == nil {
		return nil, nil
	}
	switch source := block.Source.(type) {
	case *message.Base64Source:
		if !strings.HasPrefix(source.MediaType, "image/") {
			return nil, fmt.Errorf("anthropic: unsupported base64 media type %q", source.MediaType)
		}
		part := sdk.NewImageBlockBase64(source.MediaType, source.Data)
		return &part, nil
	case *message.URLSource:
		if !strings.HasPrefix(source.MediaType, "image/") {
			return nil, fmt.Errorf("anthropic: unsupported URL media type %q", source.MediaType)
		}
		part := sdk.NewImageBlock(sdk.URLImageSourceParam{URL: source.URL})
		return &part, nil
	default:
		return nil, fmt.Errorf("anthropic: unsupported data source %T", block.Source)
	}
}

func toolCallInput(raw string) (any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	var input any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, fmt.Errorf("anthropic: invalid tool call input JSON: %w", err)
	}
	return input, nil
}

func toolResultText(output message.ToolResultOutput) string {
	if output.Raw != "" {
		return output.Raw
	}
	if len(output.Blocks) == 0 {
		return ""
	}
	text := output.Blocks.GetTextContent("")
	if text == nil {
		return ""
	}
	return *text
}

func formatTools(tools []asmodel.ToolSchema, choice *types.ToolChoice) ([]sdk.ToolUnionParam, *sdk.ToolChoiceUnionParam, error) {
	available := make([]string, 0, len(tools))
	for _, tool := range tools {
		available = append(available, tool.Function.Name)
	}
	if err := choice.Validate(available); err != nil {
		return nil, nil, err
	}
	filtered := filterTools(tools, choice)
	formatted := make([]sdk.ToolUnionParam, 0, len(filtered))
	for _, tool := range filtered {
		schema := toolInputSchema(tool.Function.Parameters)
		param := sdk.ToolParam{
			Name:        tool.Function.Name,
			InputSchema: schema,
		}
		if tool.Function.Description != "" {
			param.Description = sdk.String(tool.Function.Description)
		}
		formatted = append(formatted, sdk.ToolUnionParam{OfTool: &param})
	}
	toolChoice, err := formatToolChoice(choice)
	if err != nil {
		return nil, nil, err
	}
	return formatted, toolChoice, nil
}

func filterTools(tools []asmodel.ToolSchema, choice *types.ToolChoice) []asmodel.ToolSchema {
	if choice == nil || len(choice.Tools) == 0 {
		return tools
	}
	allowed := make(map[string]struct{}, len(choice.Tools))
	for _, name := range choice.Tools {
		allowed[name] = struct{}{}
	}
	filtered := make([]asmodel.ToolSchema, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowed[tool.Function.Name]; ok {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func formatToolChoice(choice *types.ToolChoice) (*sdk.ToolChoiceUnionParam, error) {
	if choice == nil {
		return nil, nil
	}
	var toolChoice sdk.ToolChoiceUnionParam
	switch choice.Mode {
	case string(types.ToolChoiceAuto):
		toolChoice = sdk.ToolChoiceUnionParam{OfAuto: &sdk.ToolChoiceAutoParam{}}
	case string(types.ToolChoiceNone):
		none := sdk.NewToolChoiceNoneParam()
		toolChoice = sdk.ToolChoiceUnionParam{OfNone: &none}
	case string(types.ToolChoiceRequired):
		toolChoice = sdk.ToolChoiceUnionParam{OfAny: &sdk.ToolChoiceAnyParam{}}
	case "":
		return nil, fmt.Errorf("anthropic: tool choice mode is empty")
	default:
		toolChoice = sdk.ToolChoiceParamOfTool(choice.Mode)
	}
	return &toolChoice, nil
}

func toolInputSchema(parameters map[string]any) sdk.ToolInputSchemaParam {
	schema := sdk.ToolInputSchemaParam{}
	if properties, ok := parameters["properties"]; ok {
		schema.Properties = properties
	}
	schema.Required = requiredStrings(parameters["required"])
	schema.ExtraFields = make(map[string]any)
	for key, value := range parameters {
		switch key {
		case "type", "properties", "required":
			continue
		default:
			schema.ExtraFields[key] = value
		}
	}
	if len(schema.ExtraFields) == 0 {
		schema.ExtraFields = nil
	}
	return schema
}

func requiredStrings(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
