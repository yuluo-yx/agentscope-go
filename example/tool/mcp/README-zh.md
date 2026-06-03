# MCP 工具示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例演示如何把 MCP server 接入 AgentScope Go 工具体系。

程序会启动一个本地 in-process MCP server，通过 `tool/mcp` 连接它，把 MCP tool 包装成 AgentScope `tool.Tool`，注册到 `tool.Toolkit`，然后执行工具。如果设置了 `AI_DASHSCOPE_API_KEY`，示例还会让 DashScope 自主决定调用 MCP tool，并把工具结果回填给模型生成最终回复。

## 前置条件

- Go 1.26.3 或更新版本。
- 本地 MCP 路径不需要 API Key。
- 可选：设置 `AI_DASHSCOPE_API_KEY` 后运行真实 ChatModel 工具调用闭环。
- 可选：设置 `AI_DASHSCOPE_MODEL` 覆盖 DashScope 模型，默认是 `qwen3.7-max`。

## 运行

```bash
go run .
```

离线输出会包含已连接 MCP client、包装后的工具名、直接 MCP 工具结果，以及 ChatModel 请求的 token 估算：

```text
mcp_client=people connected=true tools=mcp__people__lookup_profile direct_state=success direct_output="Ada maintains AgentScope Go examples and MCP tool integrations."
chat_tool=mcp__people__lookup_profile mode=offline chat_model=dashscope/qwen3.7-max estimated_tokens=...
```

运行真实模型路径：

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

## 关注点

- `newProfileServer` 创建一个很小的本地 MCP server，并提供只读 `lookup_profile` 工具。
- `mcptool.NewInProcessClient` 创建 AgentScope Go MCP client。
- `client.ListTools` 把 MCP tools 转成 AgentScope `tool.Tool`，命名格式是 `mcp__<server>__<tool>`。
- `tool.NewToolkit` 注册包装后的 MCP tools，直接工具调用和模型工具调用走同一条执行路径。
