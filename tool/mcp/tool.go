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

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// Tool adapts one raw MCP tool to the AgentScope tool interface.
type Tool struct {
	client      *Client
	raw         gomcp.Tool
	name        string
	inputSchema map[string]any
	readOnly    bool
}

// NewTool wraps one raw MCP tool.
func NewTool(client *Client, raw gomcp.Tool) (*Tool, error) {
	if client == nil {
		return nil, fmt.Errorf("mcp: nil client")
	}
	raw.Name = strings.TrimSpace(raw.Name)
	if raw.Name == "" {
		return nil, fmt.Errorf("mcp: raw tool name is required")
	}
	readOnly := false
	if raw.Annotations.ReadOnlyHint != nil {
		readOnly = *raw.Annotations.ReadOnlyHint
	}
	return &Tool{
		client:      client,
		raw:         raw,
		name:        qualifiedToolName(client.Name(), raw.Name),
		inputSchema: inputSchemaMap(raw),
		readOnly:    readOnly,
	}, nil
}

// Name returns the AgentScope-visible MCP tool name.
func (t *Tool) Name() string {
	return t.name
}

// Description returns the MCP tool description.
func (t *Tool) Description() string {
	return t.raw.Description
}

// InputSchema returns the MCP input schema as a JSON object.
func (t *Tool) InputSchema() map[string]any {
	return utils.CloneAnyMap(t.inputSchema)
}

// IsConcurrencySafe reports whether the tool can be called concurrently.
func (t *Tool) IsConcurrencySafe() bool {
	return false
}

// IsReadOnly reports whether the MCP tool declares readOnlyHint.
func (t *Tool) IsReadOnly() bool {
	return t.readOnly
}

// IsExternalTool reports whether this tool is executed externally by the host.
func (t *Tool) IsExternalTool() bool {
	return false
}

// IsStateInjected reports whether this tool requires AgentState injection.
func (t *Tool) IsStateInjected() bool {
	return false
}

// IsMCP reports whether this tool comes from an MCP server.
func (t *Tool) IsMCP() bool {
	return true
}

// MCPName returns the owning MCP client name.
func (t *Tool) MCPName() string {
	return t.client.Name()
}

// CheckPermissions returns the default MCP permission decision.
func (t *Tool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	if t.readOnly {
		return &permission.Decision{
			Behavior:       permission.BehaviorAllow,
			Message:        "This is a read-only MCP tool. Allowing execution.",
			DecisionReason: "MCP readOnlyHint is true",
		}, nil
	}
	return &permission.Decision{
		Behavior:       permission.BehaviorAsk,
		Message:        "MCP tools must be explicitly allowed by the user.",
		DecisionReason: "MCP tool is not read-only",
	}, nil
}

// MatchRule matches permission rules for this MCP tool.
func (t *Tool) MatchRule(ruleContent string, _ map[string]any) bool {
	ruleContent = strings.TrimSpace(ruleContent)
	if ruleContent == "" {
		return true
	}
	return ruleContent == t.name ||
		ruleContent == t.raw.Name ||
		ruleContent == t.client.Name()+"."+t.raw.Name ||
		ruleContent == t.client.Name()+":"+t.raw.Name
}

// GenerateSuggestions returns a stable allow rule for this MCP tool.
func (t *Tool) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName:    t.name,
		RuleContent: t.client.Name() + "." + t.raw.Name,
		Behavior:    permission.BehaviorAllow,
		Source:      "suggested",
	}}
}

// Execute invokes the raw MCP tool and converts its result to AgentScope blocks.
func (t *Tool) Execute(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan astool.ToolChunk, error) {
	chunks := make(chan astool.ToolChunk, 1)
	go func() {
		defer close(chunks)
		result, err := t.client.CallTool(ctx, t.raw.Name, input)
		if err != nil {
			chunks <- *astool.NewToolChunk(
				message.ContentBlockList{message.NewTextBlock(err.Error())},
				astool.WithToolChunkState(message.ToolResultError),
			)
			return
		}
		state := message.ToolResultSuccess
		if result.IsError {
			state = message.ToolResultError
		}
		chunks <- *astool.NewToolChunk(
			ConvertToolResult(result),
			astool.WithToolChunkState(state),
		)
	}()
	return chunks, nil
}

// ConvertToolResult converts an MCP tool result to AgentScope content blocks.
func ConvertToolResult(result *gomcp.CallToolResult) message.ContentBlockList {
	if result == nil {
		return message.ContentBlockList{}
	}
	blocks := ConvertContent(result.Content)
	if len(blocks) == 0 && result.StructuredContent != nil {
		blocks = append(blocks, message.NewTextBlock(jsonString(result.StructuredContent)))
	}
	return blocks
}

// ConvertContent converts MCP content blocks to AgentScope content blocks.
func ConvertContent(content []gomcp.Content) message.ContentBlockList {
	blocks := make(message.ContentBlockList, 0, len(content))
	for _, block := range content {
		switch typed := block.(type) {
		case gomcp.TextContent:
			blocks = append(blocks, message.NewTextBlock(typed.Text))
		case *gomcp.TextContent:
			blocks = append(blocks, message.NewTextBlock(typed.Text))
		case gomcp.ImageContent:
			blocks = append(blocks, message.NewDataBlock(message.NewBase64Source(typed.Data, typed.MIMEType)))
		case *gomcp.ImageContent:
			blocks = append(blocks, message.NewDataBlock(message.NewBase64Source(typed.Data, typed.MIMEType)))
		case gomcp.AudioContent:
			blocks = append(blocks, message.NewDataBlock(message.NewBase64Source(typed.Data, typed.MIMEType)))
		case *gomcp.AudioContent:
			blocks = append(blocks, message.NewDataBlock(message.NewBase64Source(typed.Data, typed.MIMEType)))
		case gomcp.EmbeddedResource:
			blocks = append(blocks, embeddedResourceBlocks(typed.Resource)...)
		case *gomcp.EmbeddedResource:
			blocks = append(blocks, embeddedResourceBlocks(typed.Resource)...)
		case gomcp.ResourceLink:
			blocks = append(blocks, message.NewDataBlock(message.NewURLSource(typed.URI, typed.MIMEType)))
		case *gomcp.ResourceLink:
			blocks = append(blocks, message.NewDataBlock(message.NewURLSource(typed.URI, typed.MIMEType)))
		}
	}
	return blocks
}

func embeddedResourceBlocks(resource gomcp.ResourceContents) message.ContentBlockList {
	switch typed := resource.(type) {
	case gomcp.TextResourceContents:
		return message.ContentBlockList{message.NewTextBlock(jsonString(typed))}
	case *gomcp.TextResourceContents:
		return message.ContentBlockList{message.NewTextBlock(jsonString(typed))}
	case gomcp.BlobResourceContents:
		return message.ContentBlockList{message.NewDataBlock(message.NewBase64Source(typed.Blob, typed.MIMEType))}
	case *gomcp.BlobResourceContents:
		return message.ContentBlockList{message.NewDataBlock(message.NewBase64Source(typed.Blob, typed.MIMEType))}
	default:
		return message.ContentBlockList{}
	}
}

func qualifiedToolName(mcpName, toolName string) string {
	return "mcp__" + mcpName + "__" + toolName
}

func inputSchemaMap(raw gomcp.Tool) map[string]any {
	var data []byte
	var err error
	if raw.RawInputSchema != nil {
		data = raw.RawInputSchema
	} else {
		data, err = json.Marshal(raw.InputSchema)
		if err != nil {
			data = nil
		}
	}
	var schema map[string]any
	if len(data) > 0 {
		_ = json.Unmarshal(data, &schema)
	}
	if schema == nil {
		schema = map[string]any{}
	}
	if schemaType, _ := schema["type"].(string); schemaType == "" {
		schema["type"] = "object"
	}
	if _, ok := schema["properties"]; !ok || schema["properties"] == nil {
		schema["properties"] = map[string]any{}
	}
	if _, ok := schema["required"]; !ok || schema["required"] == nil {
		schema["required"] = []any{}
	}
	return schema
}

func jsonString(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}
