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

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	mcptool "github.com/yuluo-yx/agentscope-go/tool/mcp"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "tool mcp example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	client, err := mcptool.NewInProcessClient(
		"people",
		newProfileServer(),
		mcptool.WithEnabledTools("lookup_profile"),
	)
	if err != nil {
		return fmt.Errorf("create MCP client: %w", err)
	}
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("connect MCP client: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	tools, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list MCP tools: %w", err)
	}
	lookup, err := findTool(tools, "mcp__people__lookup_profile")
	if err != nil {
		return err
	}
	direct, err := runTool(ctx, lookup, map[string]any{"name": "Ada"}, asstate.NewAgentState())
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(tools...)
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	directText := ""
	if text := direct.GetTextContent(); text != nil {
		directText = *text
	}

	fmt.Printf(
		"mcp_client=%s connected=%t tools=%s direct_state=%s direct_output=%q\n",
		client.Name(),
		client.IsConnected(),
		toolNames(tools),
		direct.State,
		directText,
	)
	result, err := runDashScopeToolCall(ctx, kit, asstate.NewAgentState(), "Use the mcp__people__lookup_profile tool to look up Ada, then answer with one short sentence using the tool result.")
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func newProfileServer() *mcpserver.MCPServer {
	server := mcpserver.NewMCPServer(
		"agentscope-profile-server",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	server.AddTool(
		gomcp.NewTool(
			"lookup_profile",
			gomcp.WithDescription("Look up a short profile by name."),
			gomcp.WithReadOnlyHintAnnotation(true),
			gomcp.WithString("name", gomcp.Required(), gomcp.Description("Name to look up.")),
		),
		func(_ context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			name := request.GetString("name", "AgentScope")
			profiles := map[string]string{
				"Ada":        "Ada maintains AgentScope Go examples and MCP tool integrations.",
				"AgentScope": "AgentScope Go provides agents, models, tools, state, workspace, and MCP integration.",
			}
			profile, ok := profiles[name]
			if !ok {
				profile = name + " is an AgentScope Go user."
			}
			return gomcp.NewToolResultText(profile), nil
		},
	)
	return server
}

func runTool(ctx context.Context, current tool.Tool, input map[string]any, state *asstate.AgentState) (*tool.ToolResponse, error) {
	chunks, err := current.Execute(ctx, input, state)
	if err != nil {
		return nil, fmt.Errorf("execute %s tool: %w", current.Name(), err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			return nil, fmt.Errorf("append %s tool chunk: %w", current.Name(), err)
		}
	}
	return response, nil
}

func runDashScopeToolCall(ctx context.Context, kit *tool.Toolkit, state *asstate.AgentState, prompt string) (string, error) {
	schemas, err := kit.ToolSchemas()
	if err != nil {
		return "", fmt.Errorf("build tool schemas: %w", err)
	}
	maxTokens := int64(256)
	temperature := 0.2
	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	chat, err := dashscope.NewChatModel(
		credential.NewDashScope(apiKey).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(false),
		dashscope.WithChatParameters(dashscope.ChatParameters{MaxTokens: &maxTokens, Temperature: &temperature}),
	)
	if err != nil {
		return "", fmt.Errorf("create DashScope chat model: %w", err)
	}
	user, err := message.NewUserMessage("user", prompt)
	if err != nil {
		return "", fmt.Errorf("create user message: %w", err)
	}
	messages := []*message.Message{user}
	var lastToolCall *message.ToolCallBlock
	var lastToolResponse *tool.ToolResponse
	for turn := 0; turn < 4; turn++ {
		response, err := chat.Call(ctx, asmodel.CallRequest{Messages: messages, Tools: schemas})
		if err != nil {
			return "", fmt.Errorf("call DashScope turn %d: %w", turn+1, err)
		}
		toolCall := firstToolCall(response.Content)
		if toolCall == nil {
			text := ""
			if responseText := response.GetTextContent(); responseText != nil {
				text = *responseText
			}
			if lastToolCall == nil {
				return "", fmt.Errorf("DashScope returned no tool call: %q", text)
			}
			if strings.TrimSpace(text) == "" {
				return "", fmt.Errorf("DashScope returned empty final text after %s", lastToolCall.Name)
			}
			return fmt.Sprintf("chat_tool=%s mode=live chat_model=%s input=%s state=%s response=%q", lastToolCall.Name, chat.Name(), lastToolCall.Input, lastToolResponse.State, shorten(text, 96)), nil
		}
		toolResponse, err := kit.RunTool(ctx, toolCall, state)
		if err != nil {
			return "", fmt.Errorf("run %s tool: %w", toolCall.Name, err)
		}
		assistantMessage, err := message.NewAssistantMessage("assistant", response.Content)
		if err != nil {
			return "", fmt.Errorf("create assistant message: %w", err)
		}
		toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
			message.NewToolResultBlock(toolCall.ID, toolCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
		})
		if err != nil {
			return "", fmt.Errorf("create tool result message: %w", err)
		}
		messages = append(messages, assistantMessage, toolMessage)
		lastToolCall = toolCall
		lastToolResponse = toolResponse
	}
	return "", fmt.Errorf("DashScope did not produce final text after tool calls")
}

func findTool(tools []tool.Tool, name string) (tool.Tool, error) {
	for _, current := range tools {
		if current.Name() == name {
			return current, nil
		}
	}
	return nil, fmt.Errorf("missing tool: %s", name)
}

func toolNames(tools []tool.Tool) string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return strings.Join(names, ",")
}

func schemaNames(schemas []asmodel.ToolSchema) string {
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Function.Name)
	}
	return strings.Join(names, ",")
}

func firstToolCall(blocks message.ContentBlockList) *message.ToolCallBlock {
	for _, block := range blocks {
		if toolCall, ok := block.(*message.ToolCallBlock); ok {
			return toolCall
		}
	}
	return nil
}

func shorten(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
