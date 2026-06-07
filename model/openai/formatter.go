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

package openai

import (
	"fmt"
	"strings"

	sdk "github.com/openai/openai-go"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func formatMessages(messages []*message.Message) ([]sdk.ChatCompletionMessageParamUnion, error) {
	formatted := make([]sdk.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		parts, toolCalls, toolResults, err := splitContent(msg.Content)
		if err != nil {
			return nil, err
		}
		if len(toolResults) > 0 {
			if len(parts) > 0 || len(toolCalls) > 0 {
				formatted = append(formatted, assistantMessageParam(textFromParts(parts), toolCalls))
			}
			for _, result := range toolResults {
				formatted = append(formatted, sdk.ChatCompletionMessageParamUnion{
					OfTool: &sdk.ChatCompletionToolMessageParam{
						Content:    sdk.ChatCompletionToolMessageParamContentUnion{OfString: sdk.String(toolResultText(result.Output))},
						ToolCallID: result.ID,
					},
				})
			}
			continue
		}
		switch msg.Role {
		case message.RoleSystem:
			formatted = append(formatted, systemMessageParam(textFromParts(parts), msg.Name))
		case message.RoleUser:
			formatted = append(formatted, userMessageParam(parts, msg.Name))
		case message.RoleAssistant:
			formatted = append(formatted, assistantMessageParam(textFromParts(parts), toolCalls))
		default:
			return nil, fmt.Errorf("openai: unsupported message role %q", msg.Role)
		}
	}
	return formatted, nil
}

func splitContent(blocks message.ContentBlockList) ([]sdk.ChatCompletionContentPartUnionParam, []sdk.ChatCompletionMessageToolCallParam, []*message.ToolResultBlock, error) {
	parts := make([]sdk.ChatCompletionContentPartUnionParam, 0, len(blocks))
	toolCalls := []sdk.ChatCompletionMessageToolCallParam{}
	toolResults := []*message.ToolResultBlock{}
	for _, block := range blocks {
		switch typed := block.(type) {
		case *message.TextBlock:
			parts = append(parts, sdk.TextContentPart(typed.Text))
		case *message.HintBlock:
			parts = append(parts, sdk.TextContentPart(typed.Hint))
		case *message.DataBlock:
			part, err := dataBlockPart(typed)
			if err != nil {
				return nil, nil, nil, err
			}
			if part != nil {
				parts = append(parts, *part)
			}
		case *message.ToolCallBlock:
			toolCalls = append(toolCalls, sdk.ChatCompletionMessageToolCallParam{
				ID: typed.ID,
				Function: sdk.ChatCompletionMessageToolCallFunctionParam{
					Name:      typed.Name,
					Arguments: typed.Input,
				},
			})
		case *message.ToolResultBlock:
			toolResults = append(toolResults, typed)
		case *message.ThinkingBlock:
			continue
		default:
			return nil, nil, nil, fmt.Errorf("openai: unsupported content block %T", block)
		}
	}
	return parts, toolCalls, toolResults, nil
}

func systemMessageParam(content, name string) sdk.ChatCompletionMessageParamUnion {
	msg := sdk.ChatCompletionSystemMessageParam{
		Content: sdk.ChatCompletionSystemMessageParamContentUnion{OfString: sdk.String(content)},
	}
	if name != "" {
		msg.Name = sdk.String(name)
	}
	return sdk.ChatCompletionMessageParamUnion{OfSystem: &msg}
}

func userMessageParam(parts []sdk.ChatCompletionContentPartUnionParam, name string) sdk.ChatCompletionMessageParamUnion {
	msg := sdk.ChatCompletionUserMessageParam{}
	if len(parts) == 1 {
		if text := parts[0].GetText(); text != nil {
			msg.Content = sdk.ChatCompletionUserMessageParamContentUnion{OfString: sdk.String(*text)}
		} else {
			msg.Content = sdk.ChatCompletionUserMessageParamContentUnion{OfArrayOfContentParts: parts}
		}
	} else {
		msg.Content = sdk.ChatCompletionUserMessageParamContentUnion{OfArrayOfContentParts: parts}
	}
	if name != "" {
		msg.Name = sdk.String(name)
	}
	return sdk.ChatCompletionMessageParamUnion{OfUser: &msg}
}

func assistantMessageParam(content string, toolCalls []sdk.ChatCompletionMessageToolCallParam) sdk.ChatCompletionMessageParamUnion {
	msg := sdk.ChatCompletionAssistantMessageParam{
		Content: sdk.ChatCompletionAssistantMessageParamContentUnion{OfString: sdk.String(content)},
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		if content == "" {
			msg.Content = sdk.ChatCompletionAssistantMessageParamContentUnion{}
		}
	}
	return sdk.ChatCompletionMessageParamUnion{OfAssistant: &msg}
}

func dataBlockPart(block *message.DataBlock) (*sdk.ChatCompletionContentPartUnionParam, error) {
	if block == nil || block.Source == nil {
		return nil, nil
	}
	switch source := block.Source.(type) {
	case *message.Base64Source:
		if strings.HasPrefix(source.MediaType, "video/") {
			return nil, &asmodel.CapabilityError{Model: "openai_chat", Capability: asmodel.ModelCapabilityVideo}
		}
		if strings.HasPrefix(source.MediaType, "audio/") {
			format := strings.TrimPrefix(source.MediaType, "audio/")
			part := sdk.InputAudioContentPart(sdk.ChatCompletionContentPartInputAudioInputAudioParam{
				Data:   source.Data,
				Format: format,
			})
			return &part, nil
		}
		if !strings.HasPrefix(source.MediaType, "image/") {
			return nil, &asmodel.CapabilityError{Model: "openai_chat", Capability: asmodel.ModelCapabilityGeneration}
		}
		part := sdk.ImageContentPart(sdk.ChatCompletionContentPartImageImageURLParam{
			URL: fmt.Sprintf("data:%s;base64,%s", source.MediaType, source.Data),
		})
		return &part, nil
	case *message.URLSource:
		if strings.HasPrefix(source.MediaType, "video/") {
			return nil, &asmodel.CapabilityError{Model: "openai_chat", Capability: asmodel.ModelCapabilityVideo}
		}
		if !strings.HasPrefix(source.MediaType, "image/") {
			return nil, &asmodel.CapabilityError{Model: "openai_chat", Capability: asmodel.ModelCapabilityGeneration}
		}
		part := sdk.ImageContentPart(sdk.ChatCompletionContentPartImageImageURLParam{URL: source.URL})
		return &part, nil
	default:
		return nil, fmt.Errorf("openai: unsupported data source %T", block.Source)
	}
}

func textFromParts(parts []sdk.ChatCompletionContentPartUnionParam) string {
	if len(parts) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, part := range parts {
		if text := part.GetText(); text != nil {
			builder.WriteString(*text)
		}
	}
	return builder.String()
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
