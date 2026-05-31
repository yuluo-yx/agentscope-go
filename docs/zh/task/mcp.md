# 模型上下文协议

`tool/mcp` 把 Model Context Protocol 服务连接到 AgentScope Go 工具系统。

## 提供能力

- Stdio MCP 客户端。
- HTTP MCP 客户端，支持 SSE 或 Streamable HTTP 传输选择。
- 用于本地示例和测试的进程内 MCP 客户端。
- 有状态和无状态连接模式。
- 工具启用和禁用过滤。
- MCP 工具包装为 AgentScope `tool.Tool`。
- MCP 内容转换为 `message.TextBlock` 和 `message.DataBlock`。

## 工具命名

MCP 工具使用与 Python 实现一致的命名规则：

```text
mcp__<server>__<tool>
```

例如，客户端名称为 `people`，原始工具名为 `lookup_profile`，暴露后的工具名是：

```text
mcp__people__lookup_profile
```

## 进程内示例

```go
client, err := mcp.NewInProcessClient("people", server)
if err != nil {
	panic(err)
}
if err := client.Connect(ctx); err != nil {
	panic(err)
}
defer client.Close()

tools, err := client.ListTools(ctx)
if err != nil {
	panic(err)
}

kit, err := tool.NewToolkit(tools...)
```

## 权限行为

如果 MCP 工具声明 `readOnlyHint=true`，AgentScope Go 默认允许执行。其他 MCP 工具默认需要询问。
