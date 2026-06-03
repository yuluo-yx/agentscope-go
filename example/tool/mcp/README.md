# MCP Tool Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows how to connect an MCP server to AgentScope Go tools.

The program starts a local in-process MCP server, connects it with `tool/mcp`, wraps the MCP tool as an AgentScope `tool.Tool`, registers it in a `tool.Toolkit`, and runs it. If `AI_DASHSCOPE_API_KEY` is set, the example also lets DashScope decide to call the MCP tool and then sends the tool result back to the model.

## Prerequisites

- Go 1.26.3 or newer.
- No API key is required for the local MCP path.
- Optional: set `AI_DASHSCOPE_API_KEY` to run the live ChatModel tool-call loop.
- Optional: set `AI_DASHSCOPE_MODEL` to override the DashScope model. The default is `qwen3.7-max`.

## Run

```bash
go run .
```

Offline output includes the connected MCP client, the wrapped tool name, the direct MCP tool result, and a token estimate for the ChatModel request:

```text
mcp_client=people connected=true tools=mcp__people__lookup_profile direct_state=success direct_output="Ada maintains AgentScope Go examples and MCP tool integrations."
chat_tool=mcp__people__lookup_profile mode=offline chat_model=dashscope/qwen3.7-max estimated_tokens=...
```

Run the live model path:

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

## What To Look At

- `newProfileServer` creates a small local MCP server with one read-only `lookup_profile` tool.
- `mcptool.NewInProcessClient` creates an AgentScope Go MCP client.
- `client.ListTools` converts MCP tools to AgentScope `tool.Tool` values named `mcp__<server>__<tool>`.
- `tool.NewToolkit` registers the wrapped MCP tools so direct tool calls and model tool calls use the same path.
